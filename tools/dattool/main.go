package main

import (
	"errors"
	"flag"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/v2fly/v2ray-core/v5/app/router/routercommon"
	"google.golang.org/protobuf/proto"
	"gopkg.in/yaml.v3"
)

type sourcesConfig struct {
	GeoIPDatURL   string `yaml:"geoip_dat_url"`
	GeositeDatURL string `yaml:"geosite_dat_url"`
}

type buildConfig struct {
	OutputTag string   `yaml:"output_tag"`
	Include   []string `yaml:"include"`
	Custom    []string `yaml:"custom"`
	Sanitize  []string `yaml:"sanitize"`
}

func main() {
	if len(os.Args) < 2 {
		fatalf("usage: dattool <command> [flags]")
	}

	var err error
	switch os.Args[1] {
	case "config-value":
		err = commandConfigValue(os.Args[2:])
	case "build-geoip":
		err = commandBuildGeoIP(os.Args[2:])
	case "build-geosite":
		err = commandBuildGeosite(os.Args[2:])
	case "validate-geoip":
		err = commandValidateGeoIP(os.Args[2:])
	case "validate-geosite":
		err = commandValidateGeosite(os.Args[2:])
	case "list-geoip":
		err = commandListGeoIP(os.Args[2:])
	case "list-geosite":
		err = commandListGeosite(os.Args[2:])
	case "count-geosite-keywords":
		err = commandCountGeositeKeywords(os.Args[2:])
	default:
		err = fmt.Errorf("unknown command: %s", os.Args[1])
	}
	if err != nil {
		fatalf("%v", err)
	}
}

func commandConfigValue(args []string) error {
	fs := flag.NewFlagSet("config-value", flag.ExitOnError)
	file := fs.String("file", "", "YAML file")
	key := fs.String("key", "", "key name")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *file == "" || *key == "" {
		return errors.New("--file and --key are required")
	}

	var cfg sourcesConfig
	if err := readYAML(*file, &cfg); err != nil {
		return err
	}

	switch *key {
	case "geoip_dat_url":
		fmt.Println(cfg.GeoIPDatURL)
	case "geosite_dat_url":
		fmt.Println(cfg.GeositeDatURL)
	default:
		return fmt.Errorf("unsupported key: %s", *key)
	}
	return nil
}

func commandBuildGeoIP(args []string) error {
	fs := flag.NewFlagSet("build-geoip", flag.ExitOnError)
	configPath := fs.String("config", "", "geoip YAML config")
	upstreamPath := fs.String("upstream", "", "upstream geoip.dat")
	outputPath := fs.String("output", "", "output geoip.dat")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *configPath == "" || *upstreamPath == "" || *outputPath == "" {
		return errors.New("--config, --upstream and --output are required")
	}

	cfg, err := readBuildConfig(*configPath)
	if err != nil {
		return err
	}

	var upstream routercommon.GeoIPList
	if err := readProto(*upstreamPath, &upstream); err != nil {
		return fmt.Errorf("read upstream geoip.dat: %w", err)
	}

	cidrs := make(map[string]*routercommon.CIDR)
	for _, tag := range cfg.Include {
		entry := findGeoIP(&upstream, tag)
		if entry == nil {
			return fmt.Errorf("geoip category not found in upstream: %s", tag)
		}
		fmt.Printf("[geoip] extracting tag: %s\n", tag)
		for _, cidr := range entry.Cidr {
			key, err := cidrKey(cidr)
			if err != nil {
				return fmt.Errorf("invalid upstream CIDR in %s: %w", tag, err)
			}
			cidrs[key] = proto.Clone(cidr).(*routercommon.CIDR)
		}
	}

	baseDir := filepath.Dir(*configPath)
	for _, customPath := range cfg.Custom {
		fullPath := resolvePath(baseDir, customPath)
		customCIDRs, err := readCustomCIDRs(fullPath)
		if err != nil {
			return err
		}
		for key, cidr := range customCIDRs {
			cidrs[key] = cidr
		}
	}
	if len(cidrs) == 0 {
		return errors.New("resulting geoip:proxy is empty")
	}

	keys := sortedKeys(cidrs)
	out := &routercommon.GeoIPList{
		Entry: []*routercommon.GeoIP{{
			CountryCode: cfg.outputCode(),
			Cidr:        make([]*routercommon.CIDR, 0, len(keys)),
		}},
	}
	for _, key := range keys {
		out.Entry[0].Cidr = append(out.Entry[0].Cidr, cidrs[key])
	}

	return writeProto(*outputPath, out)
}

func commandBuildGeosite(args []string) error {
	fs := flag.NewFlagSet("build-geosite", flag.ExitOnError)
	configPath := fs.String("config", "", "geosite YAML config")
	upstreamPath := fs.String("upstream", "", "upstream geosite.dat")
	outputPath := fs.String("output", "", "output geosite.dat")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *configPath == "" || *upstreamPath == "" || *outputPath == "" {
		return errors.New("--config, --upstream and --output are required")
	}

	cfg, err := readBuildConfig(*configPath)
	if err != nil {
		return err
	}

	var upstream routercommon.GeoSiteList
	if err := readProto(*upstreamPath, &upstream); err != nil {
		return fmt.Errorf("read upstream geosite.dat: %w", err)
	}

	domains := make(map[string]*routercommon.Domain)
	for _, tag := range cfg.Include {
		entry := findGeosite(&upstream, tag)
		if entry == nil {
			return fmt.Errorf("geosite category not found in upstream: %s", tag)
		}
		fmt.Printf("[geosite] extracting category: %s\n", tag)
		for _, domain := range entry.Domain {
			key := domainKey(domain)
			domains[key] = proto.Clone(domain).(*routercommon.Domain)
		}
	}

	baseDir := filepath.Dir(*configPath)
	for _, customPath := range cfg.Custom {
		fullPath := resolvePath(baseDir, customPath)
		customDomains, err := readCustomDomains(fullPath)
		if err != nil {
			return err
		}
		for key, domain := range customDomains {
			domains[key] = domain
		}
	}

	removed, err := sanitizeDomains(domains, cfg.Sanitize)
	if err != nil {
		return err
	}
	if len(cfg.Sanitize) > 0 {
		fmt.Printf("[geosite] sanitized %d rules by keywords: %s\n", removed, strings.Join(cfg.Sanitize, ", "))
	}

	if len(domains) == 0 {
		return errors.New("resulting geosite:proxy is empty")
	}

	keys := sortedKeys(domains)
	out := &routercommon.GeoSiteList{
		Entry: []*routercommon.GeoSite{{
			CountryCode: cfg.outputCode(),
			Domain:      make([]*routercommon.Domain, 0, len(keys)),
		}},
	}
	for _, key := range keys {
		out.Entry[0].Domain = append(out.Entry[0].Domain, domains[key])
	}

	return writeProto(*outputPath, out)
}

func commandValidateGeoIP(args []string) error {
	fs := flag.NewFlagSet("validate-geoip", flag.ExitOnError)
	datPath := fs.String("dat", "", "geoip.dat")
	tag := fs.String("tag", "proxy", "expected single tag")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *datPath == "" {
		return errors.New("--dat is required")
	}

	var list routercommon.GeoIPList
	if err := readProto(*datPath, &list); err != nil {
		return err
	}
	if len(list.Entry) != 1 {
		return fmt.Errorf("expected exactly one geoip tag, got %d", len(list.Entry))
	}
	if !strings.EqualFold(list.Entry[0].CountryCode, *tag) {
		return fmt.Errorf("expected geoip:%s, got geoip:%s", *tag, list.Entry[0].CountryCode)
	}
	if len(list.Entry[0].Cidr) == 0 {
		return fmt.Errorf("geoip:%s is empty", *tag)
	}
	fmt.Printf("[geoip] geoip:%s contains %d CIDR entries\n", list.Entry[0].CountryCode, len(list.Entry[0].Cidr))
	return nil
}

func commandValidateGeosite(args []string) error {
	fs := flag.NewFlagSet("validate-geosite", flag.ExitOnError)
	datPath := fs.String("dat", "", "geosite.dat")
	tag := fs.String("tag", "proxy", "expected single category")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *datPath == "" {
		return errors.New("--dat is required")
	}

	var list routercommon.GeoSiteList
	if err := readProto(*datPath, &list); err != nil {
		return err
	}
	if len(list.Entry) != 1 {
		return fmt.Errorf("expected exactly one geosite category, got %d", len(list.Entry))
	}
	if !strings.EqualFold(list.Entry[0].CountryCode, *tag) {
		return fmt.Errorf("expected geosite:%s, got geosite:%s", *tag, list.Entry[0].CountryCode)
	}
	if len(list.Entry[0].Domain) == 0 {
		return fmt.Errorf("geosite:%s is empty", *tag)
	}
	fmt.Printf("[geosite] geosite:%s contains %d rules\n", list.Entry[0].CountryCode, len(list.Entry[0].Domain))
	return nil
}

func commandListGeoIP(args []string) error {
	fs := flag.NewFlagSet("list-geoip", flag.ExitOnError)
	datPath := fs.String("dat", "", "geoip.dat")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *datPath == "" {
		return errors.New("--dat is required")
	}

	var list routercommon.GeoIPList
	if err := readProto(*datPath, &list); err != nil {
		return err
	}
	tags := make([]string, 0, len(list.Entry))
	for _, entry := range list.Entry {
		tags = append(tags, entry.CountryCode)
	}
	sort.Strings(tags)
	for _, tag := range tags {
		fmt.Println(tag)
	}
	return nil
}

func commandListGeosite(args []string) error {
	fs := flag.NewFlagSet("list-geosite", flag.ExitOnError)
	datPath := fs.String("dat", "", "geosite.dat")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *datPath == "" {
		return errors.New("--dat is required")
	}

	var list routercommon.GeoSiteList
	if err := readProto(*datPath, &list); err != nil {
		return err
	}
	tags := make([]string, 0, len(list.Entry))
	for _, entry := range list.Entry {
		tags = append(tags, entry.CountryCode)
	}
	sort.Strings(tags)
	for _, tag := range tags {
		fmt.Println(tag)
	}
	return nil
}

func commandCountGeositeKeywords(args []string) error {
	fs := flag.NewFlagSet("count-geosite-keywords", flag.ExitOnError)
	datPath := fs.String("dat", "", "geosite.dat")
	keywordsArg := fs.String("keywords", "", "comma-separated keywords")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *datPath == "" || *keywordsArg == "" {
		return errors.New("--dat and --keywords are required")
	}

	var list routercommon.GeoSiteList
	if err := readProto(*datPath, &list); err != nil {
		return err
	}

	keywords := strings.Split(*keywordsArg, ",")
	counts := make(map[string]int, len(keywords))
	for _, raw := range keywords {
		keyword := strings.ToLower(strings.TrimSpace(raw))
		if keyword == "" {
			continue
		}
		counts[keyword] = 0
		for _, entry := range list.Entry {
			for _, domain := range entry.Domain {
				if strings.Contains(strings.ToLower(domain.Value), keyword) {
					counts[keyword]++
				}
			}
		}
	}

	for _, keyword := range sortedKeys(counts) {
		fmt.Printf("%s: %d\n", keyword, counts[keyword])
	}
	return nil
}

func readBuildConfig(path string) (*buildConfig, error) {
	var cfg buildConfig
	if err := readYAML(path, &cfg); err != nil {
		return nil, err
	}
	if cfg.OutputTag == "" {
		cfg.OutputTag = "proxy"
	}
	cfg.OutputTag = strings.TrimSpace(cfg.OutputTag)
	if cfg.OutputTag == "" {
		return nil, errors.New("output_tag is empty")
	}
	return &cfg, nil
}

func (cfg *buildConfig) outputCode() string {
	return strings.ToUpper(cfg.OutputTag)
}

func readYAML(path string, out any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := yaml.Unmarshal(data, out); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	return nil
}

func readProto(path string, message proto.Message) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if len(data) == 0 {
		return fmt.Errorf("empty dat file: %s", path)
	}
	return proto.Unmarshal(data, message)
}

func writeProto(path string, message proto.Message) error {
	data, err := proto.Marshal(message)
	if err != nil {
		return err
	}
	if len(data) == 0 {
		return fmt.Errorf("refusing to write empty dat: %s", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func findGeoIP(list *routercommon.GeoIPList, tag string) *routercommon.GeoIP {
	for _, entry := range list.Entry {
		if strings.EqualFold(entry.CountryCode, tag) {
			return entry
		}
	}
	return nil
}

func findGeosite(list *routercommon.GeoSiteList, tag string) *routercommon.GeoSite {
	for _, entry := range list.Entry {
		if strings.EqualFold(entry.CountryCode, tag) {
			return entry
		}
	}
	return nil
}

func readCustomCIDRs(path string) (map[string]*routercommon.CIDR, error) {
	lines, err := readCleanLines(path)
	if err != nil {
		return nil, err
	}
	out := make(map[string]*routercommon.CIDR)
	for _, line := range lines {
		prefix, err := netip.ParsePrefix(line.value)
		if err != nil {
			return nil, fmt.Errorf("%s:%d: invalid CIDR %q: %w", path, line.number, line.value, err)
		}
		prefix = prefix.Masked()
		key := prefix.String()
		out[key] = &routercommon.CIDR{
			Ip:     prefix.Addr().AsSlice(),
			Prefix: uint32(prefix.Bits()),
		}
	}
	return out, nil
}

var domainValuePattern = regexp.MustCompile(`^[^\s#]+$`)

func readCustomDomains(path string) (map[string]*routercommon.Domain, error) {
	lines, err := readCleanLines(path)
	if err != nil {
		return nil, err
	}
	out := make(map[string]*routercommon.Domain)
	for _, line := range lines {
		domain, err := parseDomainRule(line.value)
		if err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, line.number, err)
		}
		out[domainKey(domain)] = domain
	}
	return out, nil
}

func parseDomainRule(raw string) (*routercommon.Domain, error) {
	prefix, value, hasPrefix := strings.Cut(raw, ":")
	if !hasPrefix {
		value = raw
		prefix = "domain"
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, fmt.Errorf("empty domain rule: %q", raw)
	}
	if !domainValuePattern.MatchString(value) {
		return nil, fmt.Errorf("invalid domain rule value: %q", raw)
	}

	switch prefix {
	case "domain":
		return &routercommon.Domain{Type: routercommon.Domain_RootDomain, Value: value}, nil
	case "full":
		return &routercommon.Domain{Type: routercommon.Domain_Full, Value: value}, nil
	case "keyword":
		return &routercommon.Domain{Type: routercommon.Domain_Plain, Value: value}, nil
	case "regexp":
		if _, err := regexp.Compile(value); err != nil {
			return nil, fmt.Errorf("invalid regexp rule %q: %w", raw, err)
		}
		return &routercommon.Domain{Type: routercommon.Domain_Regex, Value: value}, nil
	default:
		return nil, fmt.Errorf("unsupported rule prefix %q", prefix)
	}
}

type cleanLine struct {
	number int
	value  string
}

func readCleanLines(path string) ([]cleanLine, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	rawLines := strings.Split(string(data), "\n")
	out := make([]cleanLine, 0, len(rawLines))
	for i, raw := range rawLines {
		value := strings.TrimSpace(stripComment(raw))
		if value == "" {
			continue
		}
		out = append(out, cleanLine{number: i + 1, value: value})
	}
	return out, nil
}

func stripComment(line string) string {
	if idx := strings.Index(line, "#"); idx >= 0 {
		return line[:idx]
	}
	return line
}

func cidrKey(cidr *routercommon.CIDR) (string, error) {
	addr, ok := netip.AddrFromSlice(cidr.Ip)
	if !ok {
		return "", fmt.Errorf("invalid IP bytes: %v", cidr.Ip)
	}
	prefix := netip.PrefixFrom(addr, int(cidr.Prefix)).Masked()
	if !prefix.IsValid() {
		return "", fmt.Errorf("invalid prefix length %d for %s", cidr.Prefix, addr)
	}
	return prefix.String(), nil
}

func domainKey(domain *routercommon.Domain) string {
	return fmt.Sprintf("%d:%s", domain.Type, domain.Value)
}

func sanitizeDomains(domains map[string]*routercommon.Domain, rawKeywords []string) (int, error) {
	keywords := make([]string, 0, len(rawKeywords))
	for _, raw := range rawKeywords {
		keyword := strings.ToLower(strings.TrimSpace(raw))
		if keyword == "" {
			return 0, errors.New("sanitize keyword must not be empty")
		}
		keywords = append(keywords, keyword)
	}
	if len(keywords) == 0 {
		return 0, nil
	}

	removed := 0
	for key, domain := range domains {
		value := strings.ToLower(domain.Value)
		for _, keyword := range keywords {
			if strings.Contains(value, keyword) {
				delete(domains, key)
				removed++
				break
			}
		}
	}
	return removed, nil
}

func sortedKeys[T any](m map[string]T) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func resolvePath(baseDir, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	root := filepath.Dir(baseDir)
	return filepath.Clean(filepath.Join(root, path))
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "dattool: "+format+"\n", args...)
	os.Exit(1)
}

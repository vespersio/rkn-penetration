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

type outputConfig struct {
	OutputTag string   `yaml:"output_tag"`
	Include   []string `yaml:"include"`
	Custom    []string `yaml:"custom"`
	Sanitize  []string `yaml:"sanitize"`
}

type buildConfig struct {
	Outputs []outputConfig `yaml:"outputs"`

	// Legacy single-output format. Keep it readable so existing configs continue
	// to work while new configs use Outputs.
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

	baseDir := filepath.Dir(*configPath)
	out := &routercommon.GeoIPList{
		Entry: make([]*routercommon.GeoIP, 0, len(cfg.Outputs)),
	}
	for _, output := range cfg.Outputs {
		cidrs := make(map[string]*routercommon.CIDR)
		for _, tag := range output.Include {
			entry := findGeoIP(&upstream, tag)
			if entry == nil {
				return fmt.Errorf("geoip:%s: category not found in upstream: %s", output.OutputTag, tag)
			}
			fmt.Printf("[geoip:%s] extracting tag: %s\n", output.OutputTag, tag)
			for _, cidr := range entry.Cidr {
				key, err := cidrKey(cidr)
				if err != nil {
					return fmt.Errorf("invalid upstream CIDR in %s: %w", tag, err)
				}
				cidrs[key] = proto.Clone(cidr).(*routercommon.CIDR)
			}
		}

		for _, customPath := range output.Custom {
			fullPath := resolvePath(baseDir, customPath)
			customCIDRs, err := readCustomCIDRs(fullPath)
			if err != nil {
				return fmt.Errorf("geoip:%s: %w", output.OutputTag, err)
			}
			for key, cidr := range customCIDRs {
				cidrs[key] = cidr
			}
		}
		if len(cidrs) == 0 {
			return fmt.Errorf("resulting geoip:%s is empty", output.OutputTag)
		}

		keys := sortedKeys(cidrs)
		entry := &routercommon.GeoIP{
			CountryCode: output.outputCode(),
			Cidr:        make([]*routercommon.CIDR, 0, len(keys)),
		}
		for _, key := range keys {
			entry.Cidr = append(entry.Cidr, cidrs[key])
		}
		out.Entry = append(out.Entry, entry)
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

	baseDir := filepath.Dir(*configPath)
	out := &routercommon.GeoSiteList{
		Entry: make([]*routercommon.GeoSite, 0, len(cfg.Outputs)),
	}
	for _, output := range cfg.Outputs {
		domains := make(map[string]*routercommon.Domain)
		for _, tag := range output.Include {
			entry := findGeosite(&upstream, tag)
			if entry == nil {
				return fmt.Errorf("geosite:%s: category not found in upstream: %s", output.OutputTag, tag)
			}
			fmt.Printf("[geosite:%s] extracting category: %s\n", output.OutputTag, tag)
			for _, domain := range entry.Domain {
				key := domainKey(domain)
				domains[key] = proto.Clone(domain).(*routercommon.Domain)
			}
		}

		for _, customPath := range output.Custom {
			fullPath := resolvePath(baseDir, customPath)
			customDomains, err := readCustomDomains(fullPath)
			if err != nil {
				return fmt.Errorf("geosite:%s: %w", output.OutputTag, err)
			}
			for key, domain := range customDomains {
				domains[key] = domain
			}
		}

		removed, err := sanitizeDomains(domains, output.Sanitize)
		if err != nil {
			return fmt.Errorf("geosite:%s: %w", output.OutputTag, err)
		}
		if len(output.Sanitize) > 0 {
			fmt.Printf("[geosite:%s] sanitized %d rules by filters: %s\n", output.OutputTag, removed, strings.Join(output.Sanitize, ", "))
		}

		if len(domains) == 0 {
			return fmt.Errorf("resulting geosite:%s is empty", output.OutputTag)
		}

		keys := sortedKeys(domains)
		entry := &routercommon.GeoSite{
			CountryCode: output.outputCode(),
			Domain:      make([]*routercommon.Domain, 0, len(keys)),
		}
		for _, key := range keys {
			entry.Domain = append(entry.Domain, domains[key])
		}
		out.Entry = append(out.Entry, entry)
	}

	return writeProto(*outputPath, out)
}

func commandValidateGeoIP(args []string) error {
	fs := flag.NewFlagSet("validate-geoip", flag.ExitOnError)
	datPath := fs.String("dat", "", "geoip.dat")
	configPath := fs.String("config", "", "geoip YAML config")
	tag := fs.String("tag", "", "expected single tag (legacy validation mode)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *datPath == "" {
		return errors.New("--dat is required")
	}
	expected, err := expectedTags(*configPath, *tag)
	if err != nil {
		return err
	}

	var list routercommon.GeoIPList
	if err := readProto(*datPath, &list); err != nil {
		return err
	}
	if err := validateGeoIPTags(&list, expected); err != nil {
		return err
	}
	return nil
}

func commandValidateGeosite(args []string) error {
	fs := flag.NewFlagSet("validate-geosite", flag.ExitOnError)
	datPath := fs.String("dat", "", "geosite.dat")
	configPath := fs.String("config", "", "geosite YAML config")
	tag := fs.String("tag", "", "expected single category (legacy validation mode)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *datPath == "" {
		return errors.New("--dat is required")
	}
	expected, err := expectedTags(*configPath, *tag)
	if err != nil {
		return err
	}

	var list routercommon.GeoSiteList
	if err := readProto(*datPath, &list); err != nil {
		return err
	}
	if err := validateGeositeTags(&list, expected); err != nil {
		return err
	}
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

	hasLegacyFields := cfg.OutputTag != "" || cfg.Include != nil || cfg.Custom != nil || cfg.Sanitize != nil
	usingOutputs := cfg.Outputs != nil
	if usingOutputs && hasLegacyFields {
		return nil, errors.New("config must use either outputs or the legacy top-level output_tag/include/custom/sanitize fields, not both")
	}
	if usingOutputs && len(cfg.Outputs) == 0 {
		return nil, errors.New("outputs must contain at least one output")
	}
	if !usingOutputs {
		if strings.TrimSpace(cfg.OutputTag) == "" {
			cfg.OutputTag = "proxy"
		}
		cfg.Outputs = []outputConfig{{
			OutputTag: cfg.OutputTag,
			Include:   cfg.Include,
			Custom:    cfg.Custom,
			Sanitize:  cfg.Sanitize,
		}}
	}

	seen := make(map[string]struct{}, len(cfg.Outputs))
	for i := range cfg.Outputs {
		output := &cfg.Outputs[i]
		output.OutputTag = strings.TrimSpace(output.OutputTag)
		if output.OutputTag == "" {
			return nil, fmt.Errorf("outputs[%d].output_tag is empty", i)
		}
		key := strings.ToUpper(output.OutputTag)
		if _, ok := seen[key]; ok {
			return nil, fmt.Errorf("duplicate output_tag: %s", output.OutputTag)
		}
		seen[key] = struct{}{}
	}

	return &cfg, nil
}

func (cfg *outputConfig) outputCode() string {
	return strings.ToUpper(cfg.OutputTag)
}

func expectedTags(configPath, tag string) ([]string, error) {
	if configPath != "" && tag != "" {
		return nil, errors.New("--config and --tag are mutually exclusive")
	}
	if configPath != "" {
		cfg, err := readBuildConfig(configPath)
		if err != nil {
			return nil, err
		}
		tags := make([]string, 0, len(cfg.Outputs))
		for _, output := range cfg.Outputs {
			tags = append(tags, output.OutputTag)
		}
		return tags, nil
	}
	if tag == "" {
		tag = "proxy"
	}
	return []string{tag}, nil
}

func validateGeoIPTags(list *routercommon.GeoIPList, expected []string) error {
	if len(list.Entry) != len(expected) {
		return fmt.Errorf("expected %d geoip tags, got %d", len(expected), len(list.Entry))
	}
	actual := make(map[string]*routercommon.GeoIP, len(list.Entry))
	for _, entry := range list.Entry {
		key := strings.ToUpper(entry.CountryCode)
		if _, exists := actual[key]; exists {
			return fmt.Errorf("duplicate geoip tag in output: %s", entry.CountryCode)
		}
		actual[key] = entry
	}
	for _, tag := range expected {
		entry := actual[strings.ToUpper(tag)]
		if entry == nil {
			return fmt.Errorf("expected geoip:%s is missing", tag)
		}
		if len(entry.Cidr) == 0 {
			return fmt.Errorf("geoip:%s is empty", tag)
		}
		fmt.Printf("[geoip] geoip:%s contains %d CIDR entries\n", entry.CountryCode, len(entry.Cidr))
	}
	return nil
}

func validateGeositeTags(list *routercommon.GeoSiteList, expected []string) error {
	if len(list.Entry) != len(expected) {
		return fmt.Errorf("expected %d geosite categories, got %d", len(expected), len(list.Entry))
	}
	actual := make(map[string]*routercommon.GeoSite, len(list.Entry))
	for _, entry := range list.Entry {
		key := strings.ToUpper(entry.CountryCode)
		if _, exists := actual[key]; exists {
			return fmt.Errorf("duplicate geosite category in output: %s", entry.CountryCode)
		}
		actual[key] = entry
	}
	for _, tag := range expected {
		entry := actual[strings.ToUpper(tag)]
		if entry == nil {
			return fmt.Errorf("expected geosite:%s is missing", tag)
		}
		if len(entry.Domain) == 0 {
			return fmt.Errorf("geosite:%s is empty", tag)
		}
		fmt.Printf("[geosite] geosite:%s contains %d rules\n", entry.CountryCode, len(entry.Domain))
	}
	return nil
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

type sanitizeRule struct {
	raw   string
	kind  string
	value string
}

func sanitizeDomains(domains map[string]*routercommon.Domain, rawKeywords []string) (int, error) {
	rules := make([]sanitizeRule, 0, len(rawKeywords))
	for _, raw := range rawKeywords {
		keyword := strings.ToLower(strings.TrimSpace(raw))
		if keyword == "" {
			return 0, errors.New("sanitize keyword must not be empty")
		}
		rule := sanitizeRule{raw: raw, kind: "keyword", value: keyword}
		if strings.HasPrefix(keyword, ".") {
			rule.kind = "suffix"
		}
		rules = append(rules, rule)
	}
	if len(rules) == 0 {
		return 0, nil
	}

	removed := 0
	for key, domain := range domains {
		value := strings.ToLower(domain.Value)
		for _, rule := range rules {
			if sanitizeRuleMatches(value, rule) {
				delete(domains, key)
				removed++
				break
			}
		}
	}
	return removed, nil
}

func sanitizeRuleMatches(value string, rule sanitizeRule) bool {
	switch rule.kind {
	case "suffix":
		return strings.HasSuffix(value, rule.value)
	default:
		return strings.Contains(value, rule.value)
	}
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

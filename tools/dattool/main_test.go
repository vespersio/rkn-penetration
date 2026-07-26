package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/v2fly/v2ray-core/v5/app/router/routercommon"
	"google.golang.org/protobuf/proto"
)

func TestBuildGeoIPMultipleOutputs(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	mustMkdirAll(t, configDir)
	mustWriteFile(t, filepath.Join(root, "custom", "proxy.txt"), "192.0.2.0/24\n")
	mustWriteFile(t, filepath.Join(root, "custom", "direct.txt"), "2001:db8::/32\n")

	configPath := filepath.Join(configDir, "geoip.yml")
	mustWriteFile(t, configPath, `
outputs:
  - output_tag: proxy
    include: [blocked]
    custom: [custom/proxy.txt]
  - output_tag: direct
    include: [allowed]
    custom: [custom/direct.txt]
`)

	upstreamPath := filepath.Join(root, "upstream-geoip.dat")
	mustWriteProto(t, upstreamPath, &routercommon.GeoIPList{
		Entry: []*routercommon.GeoIP{
			{CountryCode: "BLOCKED", Cidr: []*routercommon.CIDR{{Ip: []byte{10, 0, 0, 0}, Prefix: 8}}},
			{CountryCode: "ALLOWED", Cidr: []*routercommon.CIDR{{Ip: []byte{172, 16, 0, 0}, Prefix: 12}}},
		},
	})

	outputPath := filepath.Join(root, "geoip.dat")
	if err := commandBuildGeoIP([]string{
		"--config", configPath,
		"--upstream", upstreamPath,
		"--output", outputPath,
	}); err != nil {
		t.Fatalf("commandBuildGeoIP() error = %v", err)
	}

	var got routercommon.GeoIPList
	mustReadProto(t, outputPath, &got)
	if err := validateGeoIPTags(&got, []string{"proxy", "direct"}); err != nil {
		t.Fatalf("validateGeoIPTags() error = %v", err)
	}
	if got.Entry[0].CountryCode != "PROXY" || len(got.Entry[0].Cidr) != 2 {
		t.Fatalf("proxy output = %#v, want 2 CIDRs", got.Entry[0])
	}
	if got.Entry[1].CountryCode != "DIRECT" || len(got.Entry[1].Cidr) != 2 {
		t.Fatalf("direct output = %#v, want 2 CIDRs", got.Entry[1])
	}
	assertCIDRValues(t, got.Entry[0], "10.0.0.0/8", "192.0.2.0/24")
	assertCIDRValues(t, got.Entry[1], "172.16.0.0/12", "2001:db8::/32")
}

func TestBuildGeositeMultipleOutputsHaveIndependentCustomAndSanitize(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	mustMkdirAll(t, configDir)
	mustWriteFile(t, filepath.Join(root, "custom", "proxy"), "domain:proxy-custom.example\n")
	mustWriteFile(t, filepath.Join(root, "custom", "direct"), "domain:direct-custom.example\n")

	configPath := filepath.Join(configDir, "geosite.yml")
	mustWriteFile(t, configPath, `
outputs:
  - output_tag: proxy
    include: [blocked]
    custom: [custom/proxy]
    sanitize: [porn]
  - output_tag: direct
    include: [allowed]
    custom: [custom/direct]
    sanitize: [casino]
`)

	upstreamPath := filepath.Join(root, "upstream-geosite.dat")
	mustWriteProto(t, upstreamPath, &routercommon.GeoSiteList{
		Entry: []*routercommon.GeoSite{
			{
				CountryCode: "BLOCKED",
				Domain: []*routercommon.Domain{
					{Type: routercommon.Domain_RootDomain, Value: "safe.example"},
					{Type: routercommon.Domain_RootDomain, Value: "porn.example"},
					{Type: routercommon.Domain_RootDomain, Value: "casino.proxy.example"},
				},
			},
			{
				CountryCode: "ALLOWED",
				Domain: []*routercommon.Domain{
					{Type: routercommon.Domain_RootDomain, Value: "direct.example"},
					{Type: routercommon.Domain_RootDomain, Value: "casino.example"},
					{Type: routercommon.Domain_RootDomain, Value: "porn.direct.example"},
				},
			},
		},
	})

	outputPath := filepath.Join(root, "geosite.dat")
	if err := commandBuildGeosite([]string{
		"--config", configPath,
		"--upstream", upstreamPath,
		"--output", outputPath,
	}); err != nil {
		t.Fatalf("commandBuildGeosite() error = %v", err)
	}

	var got routercommon.GeoSiteList
	mustReadProto(t, outputPath, &got)
	if err := validateGeositeTags(&got, []string{"proxy", "direct"}); err != nil {
		t.Fatalf("validateGeositeTags() error = %v", err)
	}
	assertDomainValues(t, got.Entry[0], "casino.proxy.example", "proxy-custom.example", "safe.example")
	assertDomainValues(t, got.Entry[1], "direct-custom.example", "direct.example", "porn.direct.example")
}

func TestReadBuildConfigSupportsLegacySingleOutput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yml")
	mustWriteFile(t, path, `
output_tag: legacy
include: [one]
custom: [custom/list]
sanitize: [filter]
`)

	cfg, err := readBuildConfig(path)
	if err != nil {
		t.Fatalf("readBuildConfig() error = %v", err)
	}
	if len(cfg.Outputs) != 1 || cfg.Outputs[0].OutputTag != "legacy" {
		t.Fatalf("outputs = %#v, want one legacy output", cfg.Outputs)
	}
}

func TestReadBuildConfigKeepsLegacyDefaultTag(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yml")
	mustWriteFile(t, path, "include: [one]\n")

	cfg, err := readBuildConfig(path)
	if err != nil {
		t.Fatalf("readBuildConfig() error = %v", err)
	}
	if len(cfg.Outputs) != 1 || cfg.Outputs[0].OutputTag != "proxy" {
		t.Fatalf("outputs = %#v, want one proxy output", cfg.Outputs)
	}
}

func TestReadBuildConfigRejectsDuplicateOutputTags(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yml")
	mustWriteFile(t, path, `
outputs:
  - output_tag: proxy
  - output_tag: PROXY
`)

	if _, err := readBuildConfig(path); err == nil {
		t.Fatal("readBuildConfig() error = nil, want duplicate output_tag error")
	}
}

func assertDomainValues(t *testing.T, entry *routercommon.GeoSite, want ...string) {
	t.Helper()
	if len(entry.Domain) != len(want) {
		t.Fatalf("%s has %d domains, want %d", entry.CountryCode, len(entry.Domain), len(want))
	}
	for i, value := range want {
		if entry.Domain[i].Value != value {
			t.Fatalf("%s domain[%d] = %q, want %q", entry.CountryCode, i, entry.Domain[i].Value, value)
		}
	}
}

func assertCIDRValues(t *testing.T, entry *routercommon.GeoIP, want ...string) {
	t.Helper()
	if len(entry.Cidr) != len(want) {
		t.Fatalf("%s has %d CIDRs, want %d", entry.CountryCode, len(entry.Cidr), len(want))
	}
	for i, value := range want {
		got, err := cidrKey(entry.Cidr[i])
		if err != nil {
			t.Fatal(err)
		}
		if got != value {
			t.Fatalf("%s CIDR[%d] = %q, want %q", entry.CountryCode, i, got, value)
		}
	}
}

func mustMkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	mustMkdirAll(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustWriteProto(t *testing.T, path string, message proto.Message) {
	t.Helper()
	data, err := proto.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustReadProto(t *testing.T, path string, message proto.Message) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := proto.Unmarshal(data, message); err != nil {
		t.Fatal(err)
	}
}

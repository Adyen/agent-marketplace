package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func sha256Hex(t *testing.T, b []byte) string {
	t.Helper()
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

const dslStub = `
require "json"
require "pathname"
require "fileutils"

$record = { urls: {}, shas: {}, meta: {}, fetches: [] }
$platform = { os: ENV.fetch("HB_OS"), cpu: ENV.fetch("HB_CPU") }
$stage_dir = File.join(ENV.fetch("HB_ROOT"), "stage")
$cache_dir = File.join(ENV.fetch("HB_ROOT"), "cache")
$bin_dir = File.join(ENV.fetch("HB_ROOT"), "bin")
[$stage_dir, $cache_dir, $bin_dir].each { |d| FileUtils.mkdir_p(d) }

def odie(msg); raise "odie: #{msg}"; end

class CurlDownloadStrategyError < StandardError
  def initialize(url, msg)
    super("#{url}: #{msg}")
  end
end

class NoUnzipCurlDownloadStrategy
  attr_reader :url, :meta

  def initialize(url, meta)
    @url = url
    @meta = meta
  end

  def basename
    Pathname.new(File.basename(@url.split("?").first))
  end

  def cached_location
    Pathname.new(File.join($cache_dir, "cached-input"))
  end

  def stage
    FileUtils.cp(cached_location, File.join($stage_dir, basename.to_s))
  end

  def fetch(timeout: nil)
    $record[:fetches] << { "host" => "api.github.com", "headers" => (@meta[:headers] || []).dup }
    if ENV["HB_SIMULATE_REDIRECT"] == "1"
      @meta.delete(:headers)
      $record[:fetches] << { "host" => "release-assets.githubusercontent.com", "headers" => (@meta[:headers] || []).dup }
    end
    if ENV["HB_FAIL_FETCH"] == "1"
      raise "curl error: #{(@meta[:headers] || []).inspect}"
    end
  end
end

class Bin
  def install(maps)
    maps.each do |src, dst|
      source = File.join($stage_dir, src)
      raise "staged source missing: #{src}" unless File.exist?(source)
      FileUtils.cp(source, File.join($bin_dir, dst))
    end
    $record[:install] = maps
  end
end

class Formula
  def self.desc(v); $record[:meta][:desc] = v; end
  def self.homepage(v); $record[:meta][:homepage] = v; end
  def self.version(v = nil); v ? $record[:meta][:version] = v : $record[:meta][:version]; end
  def self.license(v); $record[:meta][:license] = v; end
  def self.depends_on(v); ($record[:meta][:deps] ||= []) << v; end
  def self.url(u, opts = {}); $record[:urls]["#{$platform[:os]}-#{$platform[:cpu]}"] = [u, opts]; end
  def self.sha256(v); $record[:shas]["#{$platform[:os]}-#{$platform[:cpu]}"] = v; end
  def self.on_macos(&b); b.call if $platform[:os] == "macos"; end
  def self.on_linux(&b); b.call if $platform[:os] == "linux"; end
  def self.on_arm(&b); b.call if $platform[:cpu] == "arm"; end
  def self.on_intel(&b); b.call if $platform[:cpu] == "intel"; end
  def self.test(&b); $testblock = b; end
  def bin; Bin.new; end
  def assert_equal(*a); $record[:assert] = a; end
  def shell_output(*); "v1.2.3\n"; end
end

module OS
  def self.mac?; $platform[:os] == "macos"; end
  def self.linux?; $platform[:os] == "linux"; end
end

module Hardware
  class CPU
    def self.arch; ENV.fetch("HB_ARCH", "x86_64").to_sym; end
    def self.arm?; $platform[:cpu] == "arm"; end
    def self.intel?; $platform[:cpu] == "intel"; end
  end
end

load ARGV[0]

case ENV.fetch("HB_MODE")
when "install"
  f = AgentMarketplace.new
  %w[darwin-arm64 darwin-amd64 linux-arm64 linux-amd64].each do |sfx|
    File.write(File.join($stage_dir, "agent-marketplace-v#{AgentMarketplace.version}-#{sfx}"), "binary-#{sfx}")
  end
  begin
    f.install
  rescue StandardError, SystemExit => e
    $record[:install_error] = e.message
  end
  f.instance_eval(&$testblock)
when "stage"
  url, opts = $record[:urls]["#{$platform[:os]}-#{$platform[:cpu]}"]
  meta = (opts || {}).dup
  using = meta.delete(:using)
  File.write(File.join($cache_dir, "cached-input"), ENV.fetch("HB_BINARY_CONTENT"))
  klass = using == :nounzip ? NoUnzipCurlDownloadStrategy : using
  begin
    klass.new(url, meta).stage
    $record[:staged] = Dir.children($stage_dir)
    AgentMarketplace.new.install
    $record[:installed] = File.read(File.join($bin_dir, "agent-marketplace"))
  rescue StandardError, SystemExit => e
    $record[:install_error] = e.message
  end
when "fetch"
  s = AgentMarketplacePrivateDownloadStrategy.new(
    "https://api.github.com/repos/Adyen/agent-marketplace/releases/assets/1000", {})
  begin
    s.fetch
  rescue StandardError, SystemExit => e
    $record[:fetch_error] = e.message
  end
  $record[:headers_after] = s.meta[:headers]
end

puts JSON.dump($record)
`

func evalFormula(t *testing.T, formulaPath string, env map[string]string) map[string]any {
	t.Helper()
	ruby, err := exec.LookPath("ruby")
	if err != nil {
		t.Fatalf("formula contract tests require a ruby executable on PATH: %v", err)
	}
	root := t.TempDir()
	stub := filepath.Join(root, "stub.rb")
	if err := os.WriteFile(stub, []byte(dslStub), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(ruby, stub, formulaPath)
	cmd.Env = append(os.Environ(), "HB_MODE=install", "HB_ROOT="+root)
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("ruby eval failed: %v\n%s", err, out)
	}
	var record map[string]any
	if err := json.Unmarshal(out, &record); err != nil {
		t.Fatalf("parse record: %v\n%s", err, out)
	}
	return record
}

func generateFormula(t *testing.T, visibility string) (formulaPath string, tree map[string]string) {
	t.Helper()
	releaseJSON, dist := writeFixture(t, nil)
	out := filepath.Join(t.TempDir(), "tap")
	if err := generate("Adyen/agent-marketplace", "v1.2.3", releaseJSON, dist, out, visibility); err != nil {
		t.Fatalf("generate: %v", err)
	}
	tree = outputTree(t, out)
	return filepath.Join(out, "Formula", "agent-marketplace.rb"), tree
}

// spec: homebrew-packaging/formula-installs-the-declared-agent-marketplace-version
func TestFormulaInstallsDeclaredVersion(t *testing.T) {
	formulaPath, tree := generateFormula(t, "public")
	sha := map[string]string{}
	for _, suffix := range wantPlatforms {
		sum := sha256Hex(t, []byte("binary-"+suffix))
		sha[suffix] = sum
	}
	for _, combo := range []struct{ os, cpu, arch, suffix string }{
		{"macos", "arm", "arm64", "darwin-arm64"},
		{"macos", "intel", "x86_64", "darwin-amd64"},
		{"linux", "arm", "arm64", "linux-arm64"},
		{"linux", "intel", "x86_64", "linux-amd64"},
	} {
		rec := evalFormula(t, formulaPath, map[string]string{"HB_OS": combo.os, "HB_CPU": combo.cpu, "HB_ARCH": combo.arch})
		urls, _ := rec["urls"].(map[string]any)
		got, ok := urls[combo.os+"-"+combo.cpu].([]any)
		if !ok || len(got) != 2 {
			t.Fatalf("%s/%s: no url recorded: %v", combo.os, combo.cpu, rec)
		}
		wantURL := "https://github.com/Adyen/agent-marketplace/releases/download/v1.2.3/agent-marketplace-v1.2.3-" + combo.suffix
		if got[0] != wantURL {
			t.Errorf("%s/%s url = %v, want %s", combo.os, combo.cpu, got[0], wantURL)
		}
		shas, _ := rec["shas"].(map[string]any)
		if shas[combo.os+"-"+combo.cpu] != sha[combo.suffix] {
			t.Errorf("%s/%s sha256 = %v, want %s", combo.os, combo.cpu, shas[combo.os+"-"+combo.cpu], sha[combo.suffix])
		}
		install, _ := rec["install"].(map[string]any)
		if len(install) != 1 || install["agent-marketplace-v1.2.3-"+combo.suffix] != "agent-marketplace" {
			t.Errorf("%s/%s install = %v, want rename to agent-marketplace", combo.os, combo.cpu, install)
		}
	}
	for _, bad := range []struct{ name, os, cpu, arch string }{
		{"unsupported CPU", "linux", "intel", "ppc64"},
		{"32-bit CPU", "linux", "intel", "i386"},
		{"unknown CPU", "linux", "intel", "dunno"},
		{"unsupported OS", "windows", "intel", "x86_64"},
	} {
		t.Run(bad.name, func(t *testing.T) {
			rec := evalFormula(t, formulaPath, map[string]string{"HB_OS": bad.os, "HB_CPU": bad.cpu, "HB_ARCH": bad.arch})
			if rec["install"] != nil {
				t.Errorf("%s: install ran on unsupported platform: %v", bad.name, rec["install"])
			}
			if msg, _ := rec["install_error"].(string); msg == "" {
				t.Errorf("%s: no odie before install: %v", bad.name, rec)
			}
		})
	}
	rec := evalFormula(t, formulaPath, map[string]string{"HB_OS": "linux", "HB_CPU": "intel", "HB_ARCH": "x86_64"})
	meta, _ := rec["meta"].(map[string]any)
	if meta["version"] != "1.2.3" {
		t.Errorf("version metadata = %v, want 1.2.3 without v", meta["version"])
	}
	if meta["license"] != "Apache-2.0" {
		t.Errorf("license = %v, want Apache-2.0", meta["license"])
	}
	if deps, _ := meta["deps"].([]any); len(deps) != 1 || deps[0] != "git" {
		t.Errorf("depends_on = %v, want [git]", meta["deps"])
	}
	if a, _ := rec["assert"].([]any); len(a) != 2 || a[0] != "v1.2.3" {
		t.Errorf("version test assert = %v, want exact v1.2.3", rec["assert"])
	}
	if tree["VERSION"] != "1.2.3\n" {
		t.Errorf("VERSION = %q", tree["VERSION"])
	}
}

// spec: homebrew-packaging/formula-installs-the-declared-agent-marketplace-version
func TestFormulaStageInstallsCorrectBinary(t *testing.T) {
	combos := []struct{ os, cpu, arch, suffix string }{
		{"macos", "arm", "arm64", "darwin-arm64"},
		{"macos", "intel", "x86_64", "darwin-amd64"},
		{"linux", "arm", "arm64", "linux-arm64"},
		{"linux", "intel", "x86_64", "linux-amd64"},
	}
	for _, visibility := range []string{"public", "private", "internal"} {
		formulaPath, _ := generateFormula(t, visibility)
		for _, combo := range combos {
			t.Run(visibility+"/"+combo.suffix, func(t *testing.T) {
				want := "binary-" + combo.suffix
				rec := evalFormula(t, formulaPath, map[string]string{
					"HB_OS": combo.os, "HB_CPU": combo.cpu, "HB_ARCH": combo.arch,
					"HB_MODE": "stage", "HB_BINARY_CONTENT": want,
				})
				if msg, _ := rec["install_error"].(string); msg != "" {
					t.Fatalf("stage->install failed: %s (%v)", msg, rec)
				}
				if rec["installed"] != want {
					t.Errorf("installed bytes = %v, want %q", rec["installed"], want)
				}
				install, _ := rec["install"].(map[string]any)
				if len(install) != 1 || install["agent-marketplace-v1.2.3-"+combo.suffix] != "agent-marketplace" {
					t.Errorf("install = %v, want rename to agent-marketplace", install)
				}
				staged, _ := rec["staged"].([]any)
				if len(staged) != 1 || staged[0] != "agent-marketplace-v1.2.3-"+combo.suffix {
					t.Errorf("staged files = %v, want exact asset filename", staged)
				}
			})
		}
	}
}

// spec: homebrew-packaging/public-formula-downloads-anonymously
func TestPublicFormulaDownloadsAnonymously(t *testing.T) {
	formulaPath, _ := generateFormula(t, "public")
	data, err := os.ReadFile(formulaPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "HOMEBREW_AGENT_MARKETPLACE_TOKEN") {
		t.Error("public formula must not reference the private token")
	}
	rec := evalFormula(t, formulaPath, map[string]string{"HB_OS": "linux", "HB_CPU": "intel"})
	urls, _ := rec["urls"].(map[string]any)
	got, _ := urls["linux-intel"].([]any)
	if len(got) != 2 {
		t.Fatalf("no url recorded: %v", rec)
	}
	opts, _ := got[1].(map[string]any)
	if opts["using"] != ":nounzip" && opts["using"] != "nounzip" {
		t.Errorf("public url using = %v, want nounzip", opts["using"])
	}
	if install, _ := rec["install"].(map[string]any); len(install) != 1 {
		t.Errorf("public install requires no token and must run: %v", rec)
	}
}

// spec: homebrew-packaging/private-formula-defers-authentication-until-asset-fetch
func TestPrivateFormulaDefersAuthentication(t *testing.T) {
	formulaPath, _ := generateFormula(t, "internal")
	data, err := os.ReadFile(formulaPath)
	if err != nil {
		t.Fatal(err)
	}
	src := string(data)
	if strings.Contains(src, "secret-token-123") {
		t.Error("formula embeds a credential")
	}
	if !strings.Contains(src, "HOMEBREW_AGENT_MARKETPLACE_TOKEN") {
		t.Error("private formula must read the token env var at fetch time")
	}
	rec := evalFormula(t, formulaPath, map[string]string{
		"HB_OS": "macos", "HB_CPU": "arm", "HB_ARCH": "arm64",
		"HOMEBREW_AGENT_MARKETPLACE_TOKEN": "sentinel-abc-123",
	})
	if jsonString(t, rec) != "" && strings.Contains(jsonString(t, rec), "sentinel-abc-123") {
		t.Error("sentinel token leaked into evaluated formula record")
	}
	urls, _ := rec["urls"].(map[string]any)
	got, _ := urls["macos-arm"].([]any)
	if len(got) != 2 || got[0] != "https://api.github.com/repos/Adyen/agent-marketplace/releases/assets/1000" {
		t.Errorf("private asset url = %v", got)
	}
	if meta, _ := rec["meta"].(map[string]any); meta["version"] != "1.2.3" {
		t.Errorf("metadata evaluation with token present failed: %v", rec)
	}
	fetch := evalFormula(t, formulaPath, map[string]string{
		"HB_OS": "macos", "HB_CPU": "arm", "HB_MODE": "fetch",
		"HOMEBREW_AGENT_MARKETPLACE_TOKEN": "",
	})
	if f, _ := fetch["fetches"].([]any); len(f) != 0 {
		t.Errorf("request made before token check: %v", f)
	}
	if msg, _ := fetch["fetch_error"].(string); !strings.Contains(msg, "HOMEBREW_AGENT_MARKETPLACE_TOKEN") {
		t.Errorf("missing-token error = %q", msg)
	}
	fetch = evalFormula(t, formulaPath, map[string]string{
		"HB_OS": "macos", "HB_CPU": "arm", "HB_MODE": "fetch",
		"HOMEBREW_AGENT_MARKETPLACE_TOKEN": "secret-token-123",
	})
	fetches, _ := fetch["fetches"].([]any)
	if len(fetches) != 1 {
		t.Fatalf("fetches = %v", fetch)
	}
	joined := jsonString(t, fetches[0])
	if !strings.Contains(joined, "api.github.com") ||
		!strings.Contains(joined, "Authorization: Bearer secret-token-123") ||
		!strings.Contains(joined, "Accept: application/octet-stream") {
		t.Errorf("initial fetch request = %s", joined)
	}
	if after := jsonString(t, fetch["headers_after"]); strings.Contains(after, "Authorization") || strings.Contains(after, "octet-stream") {
		t.Errorf("temporary headers not cleaned: %s", after)
	}
	fetch = evalFormula(t, formulaPath, map[string]string{
		"HB_OS": "macos", "HB_CPU": "arm", "HB_MODE": "fetch", "HB_FAIL_FETCH": "1",
		"HOMEBREW_AGENT_MARKETPLACE_TOKEN": "secret-token-123",
	})
	if msg, _ := fetch["fetch_error"].(string); strings.Contains(msg, "secret-token-123") {
		t.Errorf("token leaked in failure diagnostics: %q", msg)
	}
	fetch = evalFormula(t, formulaPath, map[string]string{
		"HB_OS": "macos", "HB_CPU": "arm", "HB_MODE": "fetch", "HB_SIMULATE_REDIRECT": "1",
		"HOMEBREW_AGENT_MARKETPLACE_TOKEN": "secret-token-123",
	})
	fetches, _ = fetch["fetches"].([]any)
	if len(fetches) != 2 {
		t.Fatalf("redirected fetch should record initial + asset requests: %v", fetch)
	}
	if first := jsonString(t, fetches[0]); !strings.Contains(first, "Authorization: Bearer secret-token-123") {
		t.Errorf("initial API request lacks auth: %s", first)
	}
	if second := jsonString(t, fetches[1]); strings.Contains(second, "Authorization") {
		t.Errorf("redirected asset request carries auth: %s", second)
	}
}

func jsonString(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

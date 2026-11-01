package main

import (
	"reflect"
	"testing"
)

func TestParseDomainList(t *testing.T) {
	got := parseDomainList("https://portal.azure.com/#home, dev.azure.com/myorg/\nconsole.aws.amazon.com  www.Outlook.office.com; postman not-a-domain localhost dev.azure.com/myorg")
	want := []string{"portal.azure.com", "dev.azure.com/myorg", "console.aws.amazon.com", "outlook.office.com", "localhost"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

func TestParseAppList(t *testing.T) {
	got := parseAppList("Postman, C:\\Program Files\\Microsoft SQL Server\\ssms.exe\ndocker desktop; postman.exe")
	want := []string{"postman.exe", "ssms.exe", "docker desktop.exe"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

func TestSessionAllowsApply(t *testing.T) {
	cfg := testConfig()
	cfg.applySession(State{AllowDomains: []string{"portal.azure.com"}, AllowApps: []string{"docker desktop.exe"}})
	t.Setenv("TYPESAFE_API_KEY", "k")
	c := NewClassifier(cfg, goTask)
	if v := c.Classify("brave.exe", "Home - Microsoft Azure - Brave", "https://portal.azure.com/#home"); v.Cat != CatAllow || v.Source != "url" {
		t.Errorf("session site: got %+v", v)
	}
	if v := c.Classify("docker desktop.exe", "Containers - Docker Desktop", ""); v.Cat != CatAllow || v.Source != "rule" {
		t.Errorf("session app: got %+v", v)
	}
	// The base config isn't changed for the next session.
	if contains(testConfig().AllowDomains, "portal.azure.com") {
		t.Error("base config modified")
	}
}

func TestLastSetupRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if s := loadLastSetup(dir); s.Duration != "50m" {
		t.Errorf("default: %+v", s)
	}
	want := SessionSetup{Duration: "1h", Task: "deploy fix", AllowDomains: []string{"portal.azure.com"}, AllowApps: []string{"postman.exe"}}
	saveLastSetup(dir, want)
	if got := loadLastSetup(dir); !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v", got)
	}
}

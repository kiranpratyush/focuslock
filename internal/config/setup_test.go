package config

import (
	"reflect"
	"testing"
)

func TestParseDomainList(t *testing.T) {
	got := ParseDomainList("https://portal.azure.com/#home, dev.azure.com/myorg/\nconsole.aws.amazon.com  www.Outlook.office.com; postman not-a-domain localhost dev.azure.com/myorg")
	want := []string{"portal.azure.com", "dev.azure.com/myorg", "console.aws.amazon.com", "outlook.office.com", "localhost"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

func TestParseAppList(t *testing.T) {
	got := ParseAppList("Postman, C:\\Program Files\\Microsoft SQL Server\\ssms.exe\ndocker desktop; postman.exe")
	want := []string{"postman.exe", "ssms.exe", "docker desktop.exe"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

func TestLastSetupRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if s := LoadLastSetup(dir); s.Duration != "50m" {
		t.Errorf("default: %+v", s)
	}
	want := SessionSetup{Duration: "1h", Task: "deploy fix", AllowDomains: []string{"portal.azure.com"}, AllowApps: []string{"postman.exe"}}
	SaveLastSetup(dir, want)
	if got := LoadLastSetup(dir); !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v", got)
	}
}

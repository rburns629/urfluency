package analysis

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestPlatformsOf(t *testing.T) {
	cases := []struct {
		imp  string
		want []string
	}{
		{"github.com/aws/aws-sdk-go-v2/service/s3", []string{"aws"}},
		{"boto3", []string{"aws"}},
		{"google.cloud.storage", []string{"gcp"}},
		{"@azure/storage-blob", []string{"azure"}},
		{"firebase/firestore", []string{"firebase"}},
		{"@firebase/auth", []string{"firebase"}},
		{"k8s.io/client-go/kubernetes", []string{"kubernetes"}},
		{"cloudflare:workers", []string{"cloudflare"}},
		{"@supabase/supabase-js", []string{"supabase"}},
		{"snowflake.connector", []string{"snowflake"}},
		{"databricks.sdk", []string{"databricks"}},
		{"simple_salesforce", []string{"salesforce"}},
		{"transforms.api", []string{"palantir"}},
		{"@osdk/client", []string{"palantir"}},
		{"transforms", nil}, // too common a name alone
		{"github.com/bwmarrin/snowflake", nil},
		{"requests", nil},
	}
	for _, tc := range cases {
		if got := PlatformsOf([]string{tc.imp}); !slices.Equal(got, tc.want) {
			t.Errorf("PlatformsOf(%q) = %v, want %v", tc.imp, got, tc.want)
		}
	}
}

func TestPlatformsOfFile(t *testing.T) {
	tf := []byte(`provider "google" {
  project = var.project
}
resource "google_cloud_run_v2_service" "api" {
  name = "resource \"aws_s3_bucket\" \"x\""
}
resource "google_firebase_project" "app" {}
data "cloudflare_zone" "site" {}
variable "token" {
  default = "secret-value"
}
`)
	cases := []struct {
		path string
		want []string
		read bool
	}{
		{"infra/main.tf", []string{"cloudflare", "firebase", "gcp"}, true},
		{".terraform/modules/x/main.tf", nil, false},
		{"infra/prod.tfvars", nil, false},
		{"infra/prod.auto.tfvars", nil, false},
		{"cloudbuild.yaml", []string{"gcp"}, false},
		{"samconfig.toml", []string{"aws"}, false},
		{"infra/main.bicep", []string{"azure"}, false},
		{"charts/api/Chart.yaml", []string{"kubernetes"}, false},
		{"charts/api/values.yaml", nil, false},
		{"wrangler.toml", []string{"cloudflare"}, false},
		{"supabase/migrations/001.sql", []string{"supabase"}, false},
		{"force-app/main/default/classes/Account.cls", []string{"salesforce"}, false},
		{"docs/thesis.cls", nil, false},
		{"transforms-python/src/pipeline.py", []string{"palantir"}, false},
		{"group_vars/all.yml", nil, false},
		{"secrets/main.tf", nil, false},
	}
	for _, tc := range cases {
		read := false
		got := platformsOfFile(tc.path, func() []byte { read = true; return tf })
		if !slices.Equal(got, tc.want) || read != tc.read {
			t.Errorf("platformsOfFile(%q) = %v (read %v), want %v (read %v)", tc.path, got, read, tc.want, tc.read)
		}
	}
}

func TestScanDirPlatforms(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("main.go", "package main\n\nimport \"github.com/aws/aws-sdk-go-v2/service/s3\"\n\nvar _ = s3.New\n")
	write("infra/main.tf", "resource \"aws_s3_bucket\" \"b\" {}\n")
	write("samconfig.toml", "[default.deploy.parameters]\n")
	write("cloudbuild.yaml", "steps: []\n")

	res, err := ScanDir(context.Background(), dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if aws := res.Platforms["aws"]; aws.Repos != 1 || aws.Code != 1 || aws.Config != 2 || aws.Files != 3 {
		t.Errorf("aws = %+v", aws)
	}
	if gcp := res.Platforms["gcp"]; gcp.Config != 1 || gcp.Code != 0 {
		t.Errorf("gcp = %+v", gcp)
	}
	merged := Merge([]string{"a", "b"}, []Result{res, res})
	if aws := merged.Platforms["aws"]; aws.Repos != 2 || aws.Files != 6 {
		t.Errorf("merged aws = %+v", aws)
	}
}

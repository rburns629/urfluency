package analysis

import (
	"path"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/rburns629/urfluency/gitdata"
)

// PlatformUse is how much the user worked with one platform, such as AWS or
// Salesforce. Code counts owned files that import the platform's SDKs; Config
// counts its deploy and infrastructure files the user committed to.
type PlatformUse struct {
	Usage
	Code   int
	Config int
}

// Platforms names each platform, in report order.
var Platforms = []struct{ ID, Title string }{
	{"aws", "AWS"},
	{"gcp", "Google Cloud"},
	{"azure", "Azure"},
	{"firebase", "Firebase"},
	{"kubernetes", "Kubernetes"},
	{"cloudflare", "Cloudflare"},
	{"vercel", "Vercel"},
	{"supabase", "Supabase"},
	{"snowflake", "Snowflake"},
	{"databricks", "Databricks"},
	{"salesforce", "Salesforce"},
	{"palantir", "Palantir Foundry"},
}

// platformImports map import prefixes to platforms, matched as designLibraries
// are.
var platformImports = []struct {
	platform string
	prefixes []string
}{
	{"aws", []string{"github.com/aws/", "boto3", "botocore", "@aws-sdk/", "aws-sdk", "aws-lambda", "software.amazon.awssdk", "com.amazonaws", "amazon.", "aws_sdk_", "aws_config", "aws_lambda_powertools", "aws_cdk", "aws-cdk-lib", "aws/", "aws-sdk-"}},
	{"gcp", []string{"cloud.google.com/go", "google.cloud", "google.api_core", "@google-cloud/", "com.google.cloud", "functions_framework", "github.com/googlecloudplatform/", "vertexai", "google/cloud/", "google/cloud-", "google-cloud-"}},
	{"azure", []string{"github.com/azure/", "azure.", "@azure/", "com.azure", "azure_", "microsoft.azure", "microsoftazure/", "azure-storage-"}},
	{"firebase", []string{"firebase", "@firebase/", "firebase_admin", "firebase-admin", "firebase-functions", "@react-native-firebase/", "package:firebase_", "package:cloud_firestore", "com.google.firebase", "firebase.google.com/go", "firebasecore", "firebaseauth", "firebasefirestore", "kreait/firebase/"}},
	{"kubernetes", []string{"k8s.io/", "sigs.k8s.io/", "kubernetes", "@kubernetes/client-node", "io.fabric8", "kube"}},
	{"cloudflare", []string{"@cloudflare/", "cloudflare", "cloudflare:"}},
	{"vercel", []string{"@vercel/"}},
	{"supabase", []string{"@supabase/", "supabase", "github.com/supabase-community/"}},
	{"snowflake", []string{"snowflake", "snowflake-sdk", "github.com/snowflakedb/", "net.snowflake"}},
	{"databricks", []string{"databricks", "@databricks/", "github.com/databricks/", "com.databricks", "pyspark.dbutils"}},
	{"salesforce", []string{"simple_salesforce", "jsforce", "@salesforce/", "com.salesforce", "restforce"}},
	{"palantir", []string{"transforms.api", "transforms.external", "transforms.verbs", "foundry_sdk", "foundry.v2", "palantir_models", "@osdk/", "@palantir/", "com.palantir.transforms"}},
}

// PlatformsOf returns the platforms the imports use.
func PlatformsOf(imports []string) []string {
	var out []string
	for _, p := range platformImports {
		if slices.ContainsFunc(imports, func(imp string) bool {
			return slices.ContainsFunc(p.prefixes, func(pre string) bool { return importMatches(imp, pre) })
		}) {
			out = append(out, p.platform)
		}
	}
	return out
}

// platformFiles name a platform's deploy and infrastructure files by path
// alone; they are never opened, so files that hold values, such as
// samconfig.toml or Helm values, stay closed.
var platformFiles = []struct {
	platform string
	match    func(p, base string, dirs []string) bool
}{
	{"aws", func(p, base string, dirs []string) bool {
		return slices.Contains([]string{"samconfig.toml", "cdk.json", "buildspec.yml", "buildspec.yaml", "appspec.yml", "appspec.yaml", "amplify.yml", "serverless.yml", "serverless.yaml"}, base) ||
			slices.Contains(dirs, ".ebextensions")
	}},
	{"gcp", func(p, base string, dirs []string) bool {
		return base == "cloudbuild.yaml" || base == "cloudbuild.yml"
	}},
	{"azure", func(p, base string, dirs []string) bool {
		return hasExt(base, ".bicep") || slices.Contains([]string{"azure-pipelines.yml", "azure-pipelines.yaml", "azure.yaml", "azuredeploy.json"}, base)
	}},
	{"firebase", func(p, base string, dirs []string) bool {
		return slices.Contains([]string{"firebase.json", ".firebaserc", "firestore.rules", "storage.rules", "database.rules.json", "firestore.indexes.json", "apphosting.yaml"}, base)
	}},
	{"kubernetes", func(p, base string, dirs []string) bool {
		return slices.Contains([]string{"Chart.yaml", "kustomization.yaml", "kustomization.yml", "skaffold.yaml", "helmfile.yaml"}, base)
	}},
	{"cloudflare", func(p, base string, dirs []string) bool {
		return base == "wrangler.toml" || base == "wrangler.json" || base == "wrangler.jsonc"
	}},
	{"vercel", func(p, base string, dirs []string) bool { return base == "vercel.json" }},
	{"supabase", func(p, base string, dirs []string) bool { return len(dirs) > 0 && dirs[0] == "supabase" }},
	{"snowflake", func(p, base string, dirs []string) bool { return base == "snowflake.yml" }},
	{"databricks", func(p, base string, dirs []string) bool { return base == "databricks.yml" }},
	{"salesforce", func(p, base string, dirs []string) bool {
		return base == "sfdx-project.json" || slices.Contains(dirs, "force-app") ||
			(hasExt(base, ".cls", ".trigger") && (slices.Contains(dirs, "classes") || slices.Contains(dirs, "triggers")))
	}},
	{"palantir", func(p, base string, dirs []string) bool {
		return slices.ContainsFunc(dirs, func(d string) bool { return strings.HasPrefix(d, "transforms-") })
	}},
}

// terraformBlock matches the type label of a resource, data, or provider
// block. Only these labels are kept from a .tf file; attribute values,
// variable defaults, and locals are never matched.
var terraformBlock = regexp.MustCompile(`(?m)^[ \t]*(resource|data|provider)[ \t]+"([A-Za-z0-9_-]+)"`)

// terraformProviders map Terraform providers to platforms.
var terraformProviders = map[string]string{
	"aws": "aws", "awscc": "aws",
	"google": "gcp", "google-beta": "gcp",
	"azurerm": "azure", "azuread": "azure", "azapi": "azure", "azuredevops": "azure",
	"kubernetes": "kubernetes", "helm": "kubernetes", "kubectl": "kubernetes",
	"cloudflare": "cloudflare", "vercel": "vercel", "supabase": "supabase",
	"snowflake": "snowflake", "databricks": "databricks",
}

// terraformPlatforms returns the platforms a .tf file's blocks use. Firebase
// resources belong to the google provider but count as Firebase.
func terraformPlatforms(content []byte) []string {
	found := map[string]bool{}
	for _, m := range terraformBlock.FindAllSubmatch(content, -1) {
		label := string(m[2])
		provider := label
		if string(m[1]) != "provider" {
			provider, _, _ = strings.Cut(label, "_")
		}
		if p := terraformProviders[provider]; p != "" {
			if p == "gcp" && strings.HasPrefix(label, "google_firebase") {
				p = "firebase"
			}
			found[p] = true
		}
	}
	var out []string
	for p := range found {
		out = append(out, p)
	}
	slices.Sort(out)
	return out
}

// platformsOfFile returns the platforms a deploy or infrastructure file
// belongs to. read is called only for .tf files, which are read for their
// block labels; nothing else is opened.
func platformsOfFile(p string, read func() []byte) []string {
	dirs := strings.Split(path.Dir(p), "/")
	if slices.Contains(dirs, ".terraform") || inSkippedDir(p) || IsProtected(p) || slices.ContainsFunc(dirs, IsDependencyDir) {
		return nil
	}
	if dirs[0] == "." {
		dirs = nil
	}
	base := path.Base(p)
	if strings.HasSuffix(base, ".tf") {
		if content := read(); content != nil {
			return terraformPlatforms(content)
		}
		return nil
	}
	var out []string
	for _, f := range platformFiles {
		if f.match(p, base, dirs) {
			out = append(out, f.platform)
		}
	}
	return out
}

// platformConfig counts, per platform, the deploy and infrastructure files the
// user committed to since since. show reads a file at HEAD; files deleted
// since are counted by path only.
func platformConfig(added map[string]map[string]int, commits map[string]gitdata.Commit, since time.Time, show func(string) ([]byte, error)) map[string]PlatformUse {
	seen := map[string]bool{}
	out := map[string]PlatformUse{}
	for hash, perFile := range added {
		if c, ok := commits[hash]; !ok || c.Time.Before(since) {
			continue
		}
		for p := range perFile {
			if seen[p] {
				continue
			}
			seen[p] = true
			for _, platform := range platformsOfFile(p, func() []byte {
				b, err := show(p)
				if err != nil || len(b) > maxScanBytes {
					return nil
				}
				return b
			}) {
				addPlatform(out, platform, 0, 1)
			}
		}
	}
	return out
}

func addPlatform(out map[string]PlatformUse, platform string, code, config int) {
	u := out[platform]
	u.Repos = 1
	u.Code += code
	u.Config += config
	u.Files = u.Code + u.Config
	out[platform] = u
}

package analysis

import "testing"

func TestIsProtected(t *testing.T) {
	protected := []string{
		".env", ".env.example", ".env.local", "app/.env.production", "config/prod.env", ".envrc",
		"secrets/db.yaml", "deploy/credentials/gcp.json", "env/dev.sh", ".ssh/config", ".aws/credentials",
		"certs/server.pem", "tls/server.key", "keys/app.p12", "id_rsa", "id_ed25519.pub",
		".npmrc", ".pypirc", ".netrc", ".git-credentials", "credentials.json",
		"my-service-account.json", "android/app/google-services.json", "ios/GoogleService-Info.plist",
		"infra/prod.tfvars", "terraform.tfstate", "infra/terraform.tfstate.backup",
	}
	allowed := []string{
		"src/app.ts", "README.md", "docs/environment.md", "src/envelope.go", "config/settings.py",
		"package.json", "src/keyboard.ts", "internal/keys.go",
	}
	for _, p := range protected {
		if !IsProtected(p) {
			t.Errorf("%s should be protected", p)
		}
	}
	for _, p := range allowed {
		if IsProtected(p) {
			t.Errorf("%s should not be protected", p)
		}
	}
}

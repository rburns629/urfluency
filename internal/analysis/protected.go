package analysis

import (
	"path"
	"strings"
)

// Protected files are never read: not blamed, not shown, and not given to any
// analyzer or scanner. They hold secrets by design, so reading them would expose
// values even if no finding were reported.
var (
	protectedDirs = map[string]bool{
		"env": true, "envs": true, ".envs": true, "secrets": true, "credentials": true,
		".ssh": true, ".aws": true, ".azure": true, ".gnupg": true, ".kube": true, ".docker": true,
	}
	protectedExts = map[string]bool{
		".env": true, ".pem": true, ".key": true, ".p12": true, ".pfx": true, ".jks": true,
		".keystore": true, ".p8": true, ".ppk": true, ".tfvars": true, ".tfstate": true, ".kdbx": true,
	}
	protectedNames = map[string]bool{
		".envrc": true, ".npmrc": true, ".pypirc": true, ".netrc": true, ".git-credentials": true,
		".htpasswd": true, "credentials.json": true, "kubeconfig": true, ".dockercfg": true,
		"google-services.json": true, "GoogleService-Info.plist": true, "terraform.tfstate.backup": true,
	}
	protectedPrefixes = []string{".env", "id_rsa", "id_dsa", "id_ecdsa", "id_ed25519"}
)

// IsProtected reports whether a repository path names a file that holds secrets.
func IsProtected(p string) bool {
	parts := strings.Split(p, "/")
	for _, dir := range parts[:len(parts)-1] {
		if protectedDirs[dir] {
			return true
		}
	}
	base := parts[len(parts)-1]
	lower := strings.ToLower(base)
	if protectedNames[base] || protectedExts[strings.ToLower(path.Ext(base))] {
		return true
	}
	for _, prefix := range protectedPrefixes {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return strings.Contains(lower, "service-account") || strings.Contains(lower, "serviceaccount") ||
		strings.HasSuffix(lower, ".tfstate.backup")
}

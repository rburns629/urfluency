package java

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/rburns629/urfluency/analysis"
	"github.com/rburns629/urfluency/rules"
)

func findings(t *testing.T, path, src string) []string {
	t.Helper()
	got, err := Analyzer{}.Analyze(context.Background(), []analysis.File{{Path: path, Content: []byte(src)}})
	if err != nil {
		t.Fatal(err)
	}
	out := []string{}
	for _, f := range got {
		if f.Positive {
			continue // idioms are tested separately
		}
		out = append(out, fmt.Sprintf("%s:%d", f.Rule, f.Line))
	}
	sort.Strings(out)
	return out
}

// class wraps method bodies in a class so line numbers start at 2.
func class(body string) string { return "class A {\n" + body + "}\n" }

func TestRules(t *testing.T) {
	tests := []struct {
		name string
		path string
		src  string
		want []string
	}{
		{
			name: "sql concatenated with a value, not with constants or placeholders",
			path: "src/main/java/Repo.java",
			src: class(`void f(Statement st, Connection c, EntityManager em, String id) throws Exception {
  st.executeQuery("SELECT * FROM users WHERE id = " + id);
  c.prepareStatement("DELETE FROM t WHERE name = '" + name + "'");
  em.createQuery("select u from User u where u.name = '" + user.getName() + "'");
  st.executeQuery("SELECT * FROM " + TABLE + " WHERE id = ?");
  c.prepareStatement("SELECT * FROM users WHERE id = ?");
  executor.execute("job-" + id);
  log.info("select " + id);
}
`),
			want: []string{"java.sql-string-built:3", "java.sql-string-built:4", "java.sql-string-built:5"},
		},
		{
			name: "unfiltered deserialization",
			path: "src/main/java/Load.java",
			src: class(`Object f(InputStream in) throws Exception {
  return new ObjectInputStream(in).readObject();
}
Object g(InputStream in) throws Exception {
  return new java.beans.XMLDecoder(in).readObject();
}
Object h(InputStream in) throws Exception {
  ObjectInputStream s = new ObjectInputStream(in);
  s.setObjectInputFilter(FILTER);
  return s.readObject();
}
Object k(InputStream in) throws Exception {
  return new ObjectInputStream(in) { protected Class<?> resolveClass(ObjectStreamClass d) { return null; } }.readObject();
}
`),
			want: []string{"java.unsafe-deserialization:3", "java.unsafe-deserialization:6"},
		},
		{
			name: "deserialization in tests is not reported",
			path: "src/test/java/LoadTest.java",
			src:  class("Object f(InputStream in) throws Exception { return new ObjectInputStream(in).readObject(); }\n"),
			want: []string{},
		},
		{
			name: "commands built from concatenation, but not separate arguments",
			path: "src/main/java/Run.java",
			src: class(`void f(String dir) throws Exception {
  Runtime.getRuntime().exec("ls " + dir);
  new ProcessBuilder("sh", "-c", "tar czf " + dir + ".tgz " + dir);
  Runtime.getRuntime().exec(new String[]{"ls", dir});
  new ProcessBuilder("git", "--git-dir=" + dir, "status");
  Runtime.getRuntime().exec("ls " + DEFAULT_DIR);
}
`),
			want: []string{"java.command-built:3", "java.command-built:4"},
		},
		{
			name: "trust-all certificates and hostname verifiers",
			path: "src/main/java/Tls.java",
			src: class(`TrustManager tm = new X509TrustManager() {
  public void checkClientTrusted(X509Certificate[] c, String a) {}
  public void checkServerTrusted(X509Certificate[] c, String a) {
    // trust everyone
  }
  public X509Certificate[] getAcceptedIssuers() { return new X509Certificate[0]; }
};
HostnameVerifier hv = (host, session) -> true;
void f(HttpsURLConnection conn, HttpClientBuilder b) {
  conn.setHostnameVerifier((h, s) -> { return true; });
  b.setSSLHostnameVerifier(NoopHostnameVerifier.INSTANCE);
  conn.setHostnameVerifier(new HostnameVerifier() {
    public boolean verify(String h, SSLSession s) { return true; }
  });
  conn.setHostnameVerifier((h, s) -> h.endsWith(".example.com"));
  BiPredicate<String, String> any = (a, b2) -> true;
}
class Strict implements X509TrustManager {
  public void checkServerTrusted(X509Certificate[] c, String a) throws CertificateException { delegate.checkServerTrusted(c, a); }
}
`),
			want: []string{"java.trust-all-certificates:4", "java.trust-all-certificates:9", "java.trust-all-certificates:11",
				"java.trust-all-certificates:12", "java.trust-all-certificates:14"},
		},
		{
			name: "empty catch blocks, unless commented or named ignored",
			path: "src/main/java/C.java",
			src: class(`void f() {
  try { run(); } catch (IOException e) { }
  try { run(); } catch (IOException | RuntimeException e) {}
  try { run(); } catch (IOException e) { /* best effort: the cache is optional */ }
  try { run(); } catch (IOException ignored) {}
  try { run(); } catch (IOException e) { log.warn("x", e); }
}
`),
			want: []string{"java.empty-catch:3", "java.empty-catch:4"},
		},
		{
			name: "printStackTrace without a target, outside tests",
			path: "src/main/java/P.java",
			src: class(`void f() {
  try { run(); } catch (Exception e) { e.printStackTrace(); }
  try { run(); } catch (Exception e) { e.printStackTrace(writer); }
}
`),
			want: []string{"java.print-stack-trace:3"},
		},
		{
			name: "raw generic types in declarations, not in casts, instanceof, or with arguments",
			path: "src/main/java/R.java",
			src: class(`private List items = new ArrayList();
Map<String, List> index;
List get(Map m) { return null; }
void f(Object o) {
  List<String> ok = new ArrayList<>();
  if (o instanceof List) { List<?> l = (List) o; }
  Class<?> c = List.class;
  var v = List.of(1);
}
@SuppressWarnings("rawtypes")
void legacy() { List old = null; }
`),
			want: []string{"java.raw-type:2", "java.raw-type:2", "java.raw-type:3", "java.raw-type:4", "java.raw-type:4"},
		},
		{
			name: "List imported from java.awt is not raw",
			path: "src/main/java/Ui.java",
			src:  "import java.awt.List;\n" + class("List box = new List();\n"),
			want: []string{},
		},
		{
			name: "string compared by identity",
			path: "src/main/java/S.java",
			src: class(`boolean f(String s, Object o) {
  return s == "admin" || "x" != s || s.equals("ok") || o == null || s == other;
}
`),
			want: []string{"java.string-equality:3", "java.string-equality:3"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findings(t, tt.path, tt.src)
			want := append([]string{}, tt.want...)
			sort.Strings(want)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("got %v\nwant %v", got, want)
			}
		})
	}
}

func TestRulesAreInCatalog(t *testing.T) {
	ids := []string{"java.sql-string-built", "java.unsafe-deserialization", "java.command-built", "java.trust-all-certificates",
		"java.empty-catch", "java.print-stack-trace", "java.raw-type", "java.string-equality", "clean.cognitive-complexity"}
	for _, id := range ids {
		r, ok := rules.Lookup(id)
		if !ok {
			t.Errorf("%s is not in the catalog", id)
			continue
		}
		if !contains(r.Languages, "java") {
			t.Errorf("%s does not list java", id)
		}
	}
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func TestCognitiveComplexity(t *testing.T) {
	complex := class(`void f(List<Item> items) {
  for (Item a : items) {
    if (a.ok()) {
      for (Item b : a.children()) {
        if (b.ok() && b.ready()) {
          while (b.next()) {
            if (b.done() || b.failed()) { break; }
          }
        }
      }
    }
  }
}
`)
	if got := findings(t, "src/main/java/A.java", complex); !reflect.DeepEqual(got, []string{"clean.cognitive-complexity:2"}) {
		t.Errorf("complex method: got %v", got)
	}
	if got := findings(t, "src/test/java/ATest.java", complex); len(got) != 0 {
		t.Errorf("tests are exempt: got %v", got)
	}
	// A flat else-if chain stays readable and must not count as deeply nested.
	var chain strings.Builder
	chain.WriteString("String name(int k) {\n  if (k == 0) { return \"zero\"; }\n")
	for i := 1; i < 12; i++ {
		fmt.Fprintf(&chain, "  else if (k == %d) { return \"n%d\"; }\n", i, i)
	}
	chain.WriteString("  else { return \"many\"; }\n}\n")
	if got := findings(t, "src/main/java/A.java", class(chain.String())); len(got) != 0 {
		t.Errorf("else-if chain: got %v", got)
	}
}

func TestSkip(t *testing.T) {
	tests := []struct {
		path, src string
		want      bool
	}{
		{"src/main/java/A.java", "package a;\nclass A {}\n", false},
		{"target/generated-sources/A.java", "class A {}\n", true},
		{"app/build/generated/A.java", "class A {}\n", true},
		{"out/production/A.java", "class A {}\n", true},
		{".gradle/A.java", "class A {}\n", true},
		{"app/src/main/java/com/x/R.java", "class R {}\n", true},
		{"app/src/main/java/com/x/BuildConfig.java", "class BuildConfig {}\n", true},
		{"src/main/java/A.java", "// Generated by the protocol buffer compiler.  DO NOT EDIT!\nclass A {}\n", true},
		{"src/main/java/A.java", "import javax.annotation.processing.Generated;\n@Generated(\"dagger\")\nclass A {}\n", true},
		{"src/main/java/A.java", "class A {\n  @Generated\n  void excludedFromCoverage() {}\n}\n", false},
	}
	for _, tt := range tests {
		if got := (Analyzer{}).Skip(analysis.File{Path: tt.path, Content: []byte(tt.src)}); got != tt.want {
			t.Errorf("Skip(%s, %q) = %v, want %v", tt.path, tt.src, got, tt.want)
		}
	}
	if !(Analyzer{}).SkipPath("build/A.java") || (Analyzer{}).SkipPath("src/main/java/builder/A.java") {
		t.Error("SkipPath must match build directories exactly")
	}
	if analysis.ForPath("src/Main.java") == nil {
		t.Error("the Java analyzer is not registered for .java")
	}
}

func TestFrameworks(t *testing.T) {
	src := `package a;
import org.springframework.boot.SpringApplication;
import org.springframework.web.bind.annotation.RestController;
import jakarta.persistence.Entity;
import javax.servlet.http.HttpServlet;
import androidx.appcompat.app.AppCompatActivity;
import io.quarkus.runtime.Quarkus;
import io.micronaut.runtime.Micronaut;
import org.hibernate.Session;
import static org.junit.jupiter.api.Assertions.*;
import lombok.Data;
import java.util.List;
import springfield.Simpsons;
class A {}
`
	got := Analyzer{}.Frameworks(analysis.File{Path: "A.java", Content: []byte(src)})
	want := []string{"android", "hibernate", "jakarta-ee", "junit", "lombok", "micronaut", "quarkus", "spring", "spring-boot"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Frameworks = %v, want %v", got, want)
	}
	plain := Analyzer{}.Frameworks(analysis.File{Path: "B.java", Content: []byte("import org.springframework.context.ApplicationContext;\nclass B {}\n")})
	if !reflect.DeepEqual(plain, []string{"spring"}) {
		t.Errorf("plain Spring = %v, want [spring]", plain)
	}
}

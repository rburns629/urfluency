package php

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/rburns629/urfluency/analysis"
	"github.com/rburns629/urfluency/parsing"
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

func TestRules(t *testing.T) {
	tests := []struct {
		name string
		path string
		src  string
		want []string
	}{
		{
			name: "SQL built with concatenation and interpolation",
			path: "src/Repo.php",
			src: `<?php
mysqli_query($conn, "SELECT * FROM users WHERE id = " . $id);
$pdo->query("SELECT * FROM users WHERE name = '$name'");
$db->exec("DELETE FROM t WHERE id = {$req->id}");
$wpdb->get_results("SELECT * FROM {$wpdb->posts} WHERE post_author = $author");
DB::select("SELECT * FROM users WHERE email = '" . $email . "'");
$q->whereRaw("age > " . $age);
$pdo->prepare("SELECT * FROM t WHERE id = $id");
pg_query(<<<SQL
SELECT * FROM t WHERE id = $id
SQL);
`,
			want: []string{"php.sql-string-built:2", "php.sql-string-built:3", "php.sql-string-built:4", "php.sql-string-built:5",
				"php.sql-string-built:6", "php.sql-string-built:7", "php.sql-string-built:8", "php.sql-string-built:9"},
		},
		{
			name: "SQL with bound parameters, constants, casts, and table names",
			path: "src/Repo.php",
			src: `<?php
$stmt = $pdo->prepare("SELECT * FROM users WHERE id = ?");
$stmt->execute([$id]);
$pdo->query("SELECT * FROM users WHERE id = " . (int) $id);
$pdo->query("SELECT * FROM " . self::TABLE . " WHERE active = 1");
$wpdb->get_results($wpdb->prepare("SELECT * FROM {$wpdb->prefix}posts WHERE ID = %d", $id));
$pdo->query("SELECT * FROM $table WHERE id IN ($placeholders)");
mysqli_query($conn, "SELECT * FROM t WHERE id = " . intval($id));
$pdo->query($sql);
$sql = "SELECT * FROM users WHERE id = $id";
$cache->get("user:" . $id);
`,
			want: []string{},
		},
		{
			name: "eval and create_function",
			path: "src/a.php",
			src: `<?php
eval($code);
$f = create_function('$a', 'return $a;');
$x = evaluate($code);
`,
			want: []string{"php.eval:2", "php.eval:3"},
		},
		{
			name: "unserialize on request input, but not with allowed_classes or other data",
			path: "src/a.php",
			src: `<?php
$cart = unserialize($_COOKIE['cart']);
$data = unserialize(base64_decode($_POST['data']));
$ok = unserialize($_GET['x'], ['allowed_classes' => false]);
$cached = unserialize($redis->get('k'));
$json = json_decode($_POST['data'], true);
`,
			want: []string{"php.unsafe-deserialization:2", "php.unsafe-deserialization:3"},
		},
		{
			name: "shell commands built from variables",
			path: "src/a.php",
			src: `<?php
exec("convert " . $file . " out.png");
$out = shell_exec("ls $dir");
system($cmd);
passthru("tar czf " . basename($name));
$p = popen("grep {$pattern} log.txt", "r");
$list = ` + "`ls -la $dir`" + `;
`,
			want: []string{"php.command-built:2", "php.command-built:3", "php.command-built:4", "php.command-built:5",
				"php.command-built:6", "php.command-built:7"},
		},
		{
			name: "shell commands escaped or constant",
			path: "src/a.php",
			src: `<?php
exec("convert " . escapeshellarg($file) . " out.png");
$out = shell_exec("ls " . escapeshellarg($dir));
system("uptime");
exec("kill " . (int) $pid);
$list = ` + "`ls -la`" + `;
$proc = proc_open(['git', 'status'], $spec, $pipes);
$db->exec("VACUUM");
`,
			want: []string{},
		},
		{
			name: "include with a variable path",
			path: "src/a.php",
			src: `<?php
include $_GET['page'] . '.php';
require_once __DIR__ . "/views/$view.php";
include($path);
require __DIR__ . '/../vendor/autoload.php';
include_once 'header.php';
require dirname(__FILE__) . '/config.php';
`,
			want: []string{"php.include-variable-path:2", "php.include-variable-path:3", "php.include-variable-path:4"},
		},
		{
			name: "extract on request input",
			path: "src/a.php",
			src: `<?php
extract($_POST);
extract($_GET['filters']);
extract(['a' => 1]);
extract($row);
`,
			want: []string{"php.extract-request-input:2", "php.extract-request-input:3"},
		},
		{
			name: "fast hashes of passwords",
			path: "src/a.php",
			src: `<?php
$h = md5($password);
$h = sha1($salt . $_POST['pwd']);
$h = hash('sha256', $user->password);
$hash = password_hash($password, PASSWORD_DEFAULT);
$etag = md5($content);
$id = sha1(uniqid());
$sig = hash_hmac('sha256', $password, $key);
`,
			want: []string{"php.password-fast-hash:2", "php.password-fast-hash:3", "php.password-fast-hash:4"},
		},
		{
			name: "TLS verification disabled",
			path: "src/a.php",
			src: `<?php
curl_setopt($ch, CURLOPT_SSL_VERIFYPEER, false);
curl_setopt($ch, CURLOPT_SSL_VERIFYHOST, 0);
curl_setopt_array($ch, [CURLOPT_SSL_VERIFYPEER => false]);
$ctx = stream_context_create(['ssl' => ['verify_peer' => false]]);
$client = new Client(['base_uri' => $url, 'verify' => false]);
$res = $client->get('/x', ['verify' => false]);
Http::withoutVerifying()->get($url);
`,
			want: []string{"php.tls-verify-disabled:2", "php.tls-verify-disabled:3", "php.tls-verify-disabled:4",
				"php.tls-verify-disabled:5", "php.tls-verify-disabled:6", "php.tls-verify-disabled:7", "php.tls-verify-disabled:8"},
		},
		{
			name: "TLS verification kept",
			path: "src/a.php",
			src: `<?php
curl_setopt($ch, CURLOPT_SSL_VERIFYPEER, true);
curl_setopt($ch, CURLOPT_SSL_VERIFYHOST, 2);
$ctx = stream_context_create(['ssl' => ['verify_peer' => true, 'cafile' => $ca]]);
$client = new Client(['verify' => '/etc/ssl/ca.pem']);
$rules = ['verify' => false];
`,
			want: []string{},
		},
		{
			name: "request input echoed without escaping",
			path: "src/a.php",
			src: `<?php
echo $_GET['name'];
echo "Hello " . $_POST['name'];
print "<p>{$_REQUEST['q']}</p>";
echo '<form action="', $_SERVER['PHP_SELF'], '">';
echo trim($_GET['x']);
`,
			want: []string{"php.reflected-input:2", "php.reflected-input:3", "php.reflected-input:4", "php.reflected-input:5",
				"php.reflected-input:6"},
		},
		{
			name: "request input escaped, compared, or counted",
			path: "src/a.php",
			src: `<?php
echo htmlspecialchars($_GET['name'], ENT_QUOTES);
echo esc_html($_POST['name']);
echo (int) $_GET['page'];
echo $_GET['tab'] === 'a' ? 'active' : '';
echo isset($_GET['x']) ? 'yes' : 'no';
echo $_SERVER['SERVER_NAME'];
echo $name;
`,
			want: []string{},
		},
		{
			name: "strict types declared, and files that declare nothing",
			path: "config/app.php",
			src: `<?php
return ['name' => 'app', 'debug' => false];
`,
			want: []string{},
		},
		{
			name: "missing type declarations",
			path: "src/a.php",
			src: `<?php
declare(strict_types=1);

function total($items): int { return 0; }
function name(string $n) { return $n; }
class A {
    public function __construct($repo) {}
    public function ok(int ...$n): void {}
    public function make(?Repo $r = null): static { return $this; }
}
$f = function ($x) { return $x; };
$g = fn($x) => $x;
`,
			want: []string{"php.missing-type-declaration:4", "php.missing-type-declaration:5", "php.missing-type-declaration:7"},
		},
		{
			name: "types written in a docblock count",
			path: "src/a.php",
			src: `<?php
declare(strict_types=1);

/**
 * @param array<int, Item> $items
 * @param string|null $label
 * @return int
 */
function total($items, $label) { return 0; }

/**
 * @param  \Illuminate\Http\Request  $request
 */
function show($request): Response { return new Response(); }

/** @param int $a */
function half($a, $b): int { return 0; }

/** Says what it does, with no types. */
function bare($x) { return $x; }

class A {
    /**
     * @param mixed ...$args
     * @return static
     */
    #[Deprecated]
    public function with(...$args) { return $this; }
}
`,
			want: []string{"php.missing-type-declaration:17", "php.missing-type-declaration:20"},
		},
		{
			name: "missing type declarations in a test file are not reported",
			path: "tests/Unit/ATest.php",
			src: `<?php
declare(strict_types=1);
final class ATest extends TestCase { public function testIt() {} }
`,
			want: []string{},
		},
		{
			name: "loose comparison against true, false, null, and empty string",
			path: "src/a.php",
			src: `<?php
if ($user == null) {}
if ($flag != false) {}
if ('' == $name) {}
if ($a <> true) {}
if ($user === null) {}
if ($count == 0) {}
if ($a == $b) {}
if ($s == 'x') {}
`,
			want: []string{"php.loose-comparison:2", "php.loose-comparison:3", "php.loose-comparison:4", "php.loose-comparison:5"},
		},
		{
			name: "error suppression, but not in tests",
			path: "src/a.php",
			src: `<?php
$data = @file_get_contents($path);
$x = $y;
`,
			want: []string{"php.error-suppression:2"},
		},
		{
			name: "global variables, but not WordPress's API globals",
			path: "src/a.php",
			src: `<?php
declare(strict_types=1);
function a(): void { global $config; }
function b(): void { global $wpdb, $post; }
function c(): void { global $wpdb, $settings; }
`,
			want: []string{"php.global-variable:3", "php.global-variable:5"},
		},
		{
			name: "empty catch, but not with a comment, a body, or an ignored name",
			path: "src/a.php",
			src: `<?php
try { a(); } catch (Exception $e) { }
try { a(); } catch (IOException $e) {
    // the cache is optional
}
try { a(); } catch (Exception $e) { log($e); }
try { a(); } catch (Exception $ignored) { }
`,
			want: []string{"php.empty-catch:2"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := findings(t, tt.path, tt.src); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

// TestTemplate checks that HTML with embedded PHP, in both block and
// alternative syntax, parses without spurious findings, and that unescaped
// input in a short echo tag is still found.
func TestTemplate(t *testing.T) {
	safe := `<!DOCTYPE html>
<html>
<head><title><?= htmlspecialchars($title, ENT_QUOTES) ?></title></head>
<body>
<?php if ($user !== null): ?>
  <p class="<?= $active ? 'on' : 'off' ?>">Hello, <?= htmlspecialchars($user->name) ?></p>
<?php elseif ($guest): ?>
  <p>Welcome</p>
<?php else: ?>
  <a href="/login">Log in</a>
<?php endif; ?>
<ul>
<?php foreach ($items as $item): ?>
  <li><?php echo esc_html($item['label']); ?></li>
<?php endforeach; ?>
</ul>
<script>if (a && b) { x = 1; }</script>
</body>
</html>
`
	for _, path := range []string{"templates/page.php", "views/page.phtml"} {
		if got := findings(t, path, safe); len(got) != 0 {
			t.Errorf("%s: got %v", path, got)
		}
	}
	unsafe := "<html>\n<body>\n<p>You searched for <?= $_GET['q'] ?></p>\n</body>\n</html>\n"
	if got := findings(t, "templates/search.php", unsafe); !reflect.DeepEqual(got, []string{"php.reflected-input:3"}) {
		t.Errorf("unescaped short echo: got %v", got)
	}
}

func TestRulesAreInCatalog(t *testing.T) {
	ids := []string{"php.sql-string-built", "php.eval", "php.unsafe-deserialization", "php.command-built",
		"php.include-variable-path", "php.extract-request-input", "php.password-fast-hash", "php.tls-verify-disabled",
		"php.reflected-input", "php.missing-type-declaration", "php.loose-comparison",
		"php.error-suppression", "php.global-variable", "php.empty-catch"}
	for _, id := range ids {
		r, ok := rules.Lookup(id)
		if !ok || r.Category == rules.Idiom {
			t.Errorf("%s is not a rule in the catalog", id)
			continue
		}
		found := false
		for _, l := range r.Languages {
			found = found || l == "php"
		}
		if !found {
			t.Errorf("%s does not list php", id)
		}
	}
	if _, ok := rules.Lookup("clean.cognitive-complexity"); !ok {
		t.Error("clean.cognitive-complexity is not in the catalog")
	}
}

func TestCognitiveComplexity(t *testing.T) {
	// Each nested structure adds 1 plus its depth: 1+2+3+4+5 = 15 is at the
	// threshold; the && in the deepest condition takes it over.
	body := func(cond string) string {
		return `<?php
declare(strict_types=1);
function deep(bool $a, bool $b): void {
    if ($a) {
        foreach ([1, 2] as $i) {
            while ($b) {
                switch ($i) {
                    case 1: if (` + cond + `) { echo $i; }
                }
            }
        }
    }
}
function simple(bool $a): int { return $a ? 1 : 2; }
`
	}
	if got := findings(t, "src/a.php", body("$a && $b")); !reflect.DeepEqual(got, []string{"clean.cognitive-complexity:3"}) {
		t.Errorf("complex function: got %v", got)
	}
	if got := findings(t, "src/a.php", body("$a")); len(got) != 0 {
		t.Errorf("function at the threshold: got %v", got)
	}
	if got := findings(t, "tests/DeepTest.php", body("$a && $b")); len(got) != 0 {
		t.Errorf("test file: got %v", got)
	}
}

func TestCognitiveComplexityCounts(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want int
	}{
		{
			// if +1, && +1, elseif +1, or +1, the ternary in the elseif +2,
			// else +1.
			name: "branches and logical sequences",
			src: `<?php
function f($a, $b, $c) {
    if ($a && $b) {
        return 1;
    } elseif ($b or $c) {
        return $c ? 2 : 3;
    } else {
        return 4;
    }
}`,
			want: 7,
		},
		{
			// for +1, nested closure deepens nesting: if +3, catch +1 (top
			// level of the try), break 2 +1, match +2 inside the loop.
			name: "closures, catch, multi-level break, and match",
			src: `<?php
class A {
    public function g(array $xs): void {
        for ($i = 0; $i < 3; $i++) {
            array_map(function ($x) {
                if ($x) { return 1; }
            }, $xs);
            foreach ($xs as $x) { break 2; }
            $y = match ($i) { 1 => 'a', default => 'b' };
        }
        try { h(); } catch (Exception $e) { log($e); }
    }
}`,
			want: 1 + 3 + 2 + 1 + 2 + 1,
		},
		{
			// else if counts once for the else: if +1, else +1, the inner if is
			// not counted again; recursion +1.
			name: "else if and recursion",
			src: `<?php
function fact(int $n): int {
    if ($n < 0) { return 0; } else if ($n === 0) { return 1; }
    return $n * fact($n - 1);
}`,
			want: 3,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := complexityOf(t, tt.src); got != tt.want {
				t.Errorf("complexity = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestSkip(t *testing.T) {
	tests := []struct {
		path, src string
		want      bool
	}{
		{"src/Service.php", "<?php\nnamespace App;\n", false},
		{"vendor/laravel/framework/src/A.php", "<?php\n", true},
		{"storage/framework/views/3f2a.php", "<?php\n", true},
		{"bootstrap/cache/services.php", "<?php\n", true},
		{"var/cache/dev/Container.php", "<?php\n", true},
		{"resources/views/welcome.blade.php", "<div>{{ $x }}</div>\n", true},
		{"src/A.php", "<?php\n// Code generated by protoc. DO NOT EDIT.\n", true},
		{"src/A.php", "<?php\n/** @ORM\\GeneratedValue */\nclass E {}\n", false},
		{"src/A.php", "<?php\n" + strings.Repeat("\n", 600) + "// DO NOT EDIT below\n", false},
	}
	for _, tt := range tests {
		if got := (Analyzer{}).Skip(analysis.File{Path: tt.path, Content: []byte(tt.src)}); got != tt.want {
			t.Errorf("Skip(%s, %.30q) = %v, want %v", tt.path, tt.src, got, tt.want)
		}
	}
	if !(Analyzer{}).SkipPath("app/vendor/x/A.php") || (Analyzer{}).SkipPath("src/Vendor/A.php") || (Analyzer{}).SkipPath("app/storage/A.php") {
		t.Error("SkipPath must exclude vendor/ and framework caches only")
	}
}

func TestFrameworks(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []string
	}{
		{
			name: "use statements, grouped and aliased",
			src: `<?php
namespace App;
use Illuminate\Support\Facades\DB;
use Symfony\{Component\HttpFoundation\Request, Component\Routing\Route as R};
use Slim\Factory\AppFactory;
use CodeIgniter\Controller;
use yii\web\Controller as YiiController;
use Laminas\Mvc\Controller\AbstractActionController;
use SlimFake\Thing;
use App\Models\User;
`,
			want: []string{"codeigniter", "laminas", "laravel", "slim", "symfony", "yii"},
		},
		{
			name: "fully qualified names",
			src:  "<?php\n$r = \\Illuminate\\Support\\Facades\\Route::get('/', fn () => 'ok');\n",
			want: []string{"laravel"},
		},
		{
			name: "WordPress hooks and functions",
			src: `<?php
add_action('init', 'my_plugin_init');
function my_plugin_init(): void { wp_enqueue_script('x', plugins_url('x.js', __FILE__)); }
`,
			want: []string{"wordpress"},
		},
		{
			name: "CodeIgniter 3 base classes",
			src:  "<?php\nclass Blog extends CI_Controller { public function index() { $this->load->view('blog'); } }\n",
			want: []string{"codeigniter"},
		},
		{
			name: "plain PHP",
			src:  "<?php\nuse App\\Service;\n$x = array_map('trim', $lines);\n",
			want: []string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Analyzer{}.Frameworks(analysis.File{Path: "A.php", Content: []byte(tt.src)})
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRegistered(t *testing.T) {
	for _, p := range []string{"src/Service.php", "views/index.phtml"} {
		if a := analysis.ForPath(p); a == nil || a.Language() != "php" {
			t.Errorf("ForPath(%s) = %v", p, a)
		}
	}
}

// complexityOf returns the cognitive complexity of the only function in src.
func complexityOf(t *testing.T, src string) int {
	t.Helper()
	tree, err := parsing.Parse(parsing.PHP, []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	defer tree.Close()
	fns := parsing.Cognitive(tree, complexity)
	if len(fns) != 1 {
		t.Fatalf("got %d functions, want 1", len(fns))
	}
	return fns[0].Complexity
}

package csharp

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
			name: "SQL built with interpolation, concatenation, or Format",
			path: "Data/Repo.cs",
			src: `class Repo {
    void Run(string name) {
        var a = new SqlCommand($"SELECT * FROM t WHERE n = '{name}'", conn);
        var b = new NpgsqlCommand("SELECT * FROM t WHERE n = '" + name + "'", conn);
        db.Users.FromSqlRaw($"SELECT * FROM Users WHERE Name = {name}");
        ctx.Database.ExecuteSqlRaw(string.Format("DELETE FROM t WHERE n = '{0}'", name));
        cmd.CommandText = "SELECT * FROM t WHERE n = " + name;
    }
}
`,
			want: []string{"csharp.sql-string-built:3", "csharp.sql-string-built:4", "csharp.sql-string-built:5",
				"csharp.sql-string-built:6", "csharp.sql-string-built:7"},
		},
		{
			name: "parameterized and constant SQL is fine",
			path: "Data/Repo.cs",
			src: `class Repo {
    void Run(string name) {
        var a = new SqlCommand("SELECT * FROM t WHERE n = @n", conn);
        var b = new SqlCommand("SELECT * " + "FROM t", conn);
        var c = new SqlCommand($"SELECT 1", conn);
        db.Users.FromSqlRaw("SELECT * FROM Users WHERE Name = {0}", name);
        db.Users.FromSql($"SELECT * FROM Users WHERE Name = {name}");
        ctx.Database.ExecuteSqlInterpolated($"DELETE FROM t WHERE n = {name}");
        cmd.CommandText = sql;
        var s = new SqlCommand(sql, conn);
    }
}
`,
			want: []string{},
		},
		{
			name: "insecure serializers",
			path: "Io/Store.cs",
			src: `class Store {
    object Load(Stream s) {
        var a = new BinaryFormatter();
        var b = new System.Runtime.Serialization.Formatters.Soap.SoapFormatter();
        var c = new NetDataContractSerializer();
        var d = new DataContractSerializer(typeof(Foo));
        return JsonSerializer.Deserialize<Foo>(s);
    }
}
`,
			want: []string{"csharp.binary-formatter:3", "csharp.binary-formatter:4", "csharp.binary-formatter:5"},
		},
		{
			name: "certificate validation disabled",
			path: "Net/Client.cs",
			src: `class Client {
    void Make() {
        handler.ServerCertificateCustomValidationCallback = (m, c, ch, e) => true;
        ServicePointManager.ServerCertificateValidationCallback += delegate { return true; };
        var h = new HttpClientHandler { ServerCertificateCustomValidationCallback = (a, b, c, d) => { return true; } };
        handler.ServerCertificateCustomValidationCallback = HttpClientHandler.DangerousAcceptAnyServerCertificateValidator;
        handler.ServerCertificateCustomValidationCallback = (m, c, ch, e) => e == SslPolicyErrors.None;
        handler.ServerCertificateCustomValidationCallback = (m, c, ch, e) => { log(e); return true; };
        enabled = true;
    }
}
`,
			want: []string{"csharp.certificate-validation-disabled:3", "csharp.certificate-validation-disabled:4",
				"csharp.certificate-validation-disabled:5", "csharp.certificate-validation-disabled:6"},
		},
		{
			name: "async void outside event handlers and overrides",
			path: "App/Page.cs",
			src: `class Page {
    async void Load() { await Task.Delay(1); }
    async void OnClick(object sender, EventArgs e) { await Task.Delay(1); }
    void Handler(object sender, RoutedEventArgs e) { }
    private async void OnTapped(object sender, TappedRoutedEventArgs e) { await Task.Delay(1); }
    protected override async void OnAppearing() { await Task.Delay(1); }
    async Task Good() { await Task.Delay(1); }
    void M() {
        async void Local() { await Task.Delay(1); }
    }
}
`,
			want: []string{"csharp.async-void:2", "csharp.async-void:9"},
		},
		{
			name: "Unity messages may be async void",
			path: "Assets/Player.cs",
			src: `using UnityEngine;
class Player : MonoBehaviour {
    async void Start() { await Task.Yield(); }
    async void Fire() { await Task.Yield(); }
}
`,
			want: []string{"csharp.async-void:4"},
		},
		{
			name: "blocking on tasks",
			path: "Svc/Sync.cs",
			src: `class Sync {
    void Run() {
        var a = client.GetStringAsync(url).Result;
        client.SendAsync(req).Wait();
        var b = LoadAsync().GetAwaiter().GetResult();
        var c = Task.Run(() => 1).Result;
        Task.WaitAll(t1, t2);
        var d = validation.Result;
        var e = Validate().Result;
        semaphore.Wait();
        Monitor.Wait(gate);
    }
}
`,
			want: []string{"csharp.blocking-on-task:3", "csharp.blocking-on-task:4", "csharp.blocking-on-task:5",
				"csharp.blocking-on-task:6", "csharp.blocking-on-task:7"},
		},
		{
			name: "empty catch of everything or Exception",
			path: "Svc/Worker.cs",
			src: `class Worker {
    void Run() {
        try { Go(); } catch { }
        try { Go(); } catch (Exception) { }
        try { Go(); } catch (System.Exception ex) { }
        try { Go(); } catch (OperationCanceledException) { }
        try { Go(); } catch (Exception ex) { Log(ex); }
        try { Go(); } catch (Exception) { /* best effort: cache warmup */ }
        try { Go(); } catch (Exception e) when (e is IOException) { }
    }
}
`,
			want: []string{"csharp.empty-catch:3", "csharp.empty-catch:4", "csharp.empty-catch:5"},
		},
		{
			name: "dynamic used as a type",
			path: "Svc/Loose.cs",
			src: `class Loose {
    dynamic field;
    List<dynamic> Get(dynamic p) {
        var x = (dynamic)p;
        dynamic local = 1;
        var dynamic = 2;
        return Parse(dynamic);
    }
}
`,
			want: []string{"csharp.dynamic-type:2", "csharp.dynamic-type:3", "csharp.dynamic-type:3",
				"csharp.dynamic-type:4", "csharp.dynamic-type:5"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := findings(t, tc.path, tc.src); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v\nwant %v", got, tc.want)
			}
		})
	}
}

func TestRulesAreInCatalog(t *testing.T) {
	ids := []string{"csharp.sql-string-built", "csharp.binary-formatter", "csharp.certificate-validation-disabled",
		"csharp.async-void", "csharp.blocking-on-task", "csharp.empty-catch", "csharp.dynamic-type", "clean.cognitive-complexity"}
	for _, id := range ids {
		r, ok := rules.Lookup(id)
		if !ok || r.Category == rules.Idiom {
			t.Errorf("%s is not a problem rule in the catalog", id)
			continue
		}
		if !strings.HasPrefix(id, "clean.") && (len(r.Languages) != 1 || r.Languages[0] != "csharp") {
			t.Errorf("%s languages = %v, want [csharp]", id, r.Languages)
		}
	}
}

// complexMethod nests ifs and loops past the threshold of 15.
const complexMethod = `class Big {
    int Score(int[] xs, bool a, bool b) {
        int s = 0;
        foreach (var x in xs) {
            if (x > 0) {
                for (int i = 0; i < x; i++) {
                    if (a && b) {
                        while (s < 10) {
                            s = b ? s + 1 : s + 2;
                        }
                    }
                }
            }
        }
        return s;
    }

    int Small(int x) => x > 0 ? x : -x;
}
`

func TestCognitiveComplexity(t *testing.T) {
	if got, want := findings(t, "Src/Big.cs", complexMethod), []string{"clean.cognitive-complexity:2"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if got := findings(t, "Src/Api.Tests/BigTests.cs", complexMethod); len(got) != 0 {
		t.Errorf("test files are exempt from complexity, got %v", got)
	}
}

func TestComplexityElseIf(t *testing.T) {
	// if (+1), else if (not a new nesting level), &&: the else if is not
	// counted as nested inside the first if.
	src := `class C {
    int F(int x, bool a, bool b) {
        if (x > 0) { return 1; }
        else if (a && b) { return 2; }
        else { return 3; }
    }
}
`
	tree, err := parsing.Parse(parsing.CSharp, []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	defer tree.Close()
	got := parsing.Cognitive(tree, complexity)
	if len(got) != 1 || got[0].Complexity != 2 {
		t.Errorf("complexity = %v, want [2]", got)
	}
}

func TestSkip(t *testing.T) {
	a := Analyzer{}
	skipped := []string{
		"src/App/bin/Debug/net8.0/App.cs", "src/App/obj/Debug/App.AssemblyInfo.cs", "Forms/Main.Designer.cs",
		"Gen/Model.g.cs", "Views/MainWindow.g.i.cs", "Properties/AssemblyInfo.cs",
		"Data/Migrations/AppDbContextModelSnapshot.cs", "Data/Migrations/20240101120000_Initial.cs",
	}
	for _, p := range skipped {
		if !a.SkipPath(p) || !a.Skip(analysis.File{Path: p}) {
			t.Errorf("%s should be skipped", p)
		}
	}
	kept := []string{"src/App/Program.cs", "Data/Migrations/Helpers.cs", "Bins/Box.cs", "Data/20240101120000_Initial.cs"}
	for _, p := range kept {
		if a.SkipPath(p) || a.Skip(analysis.File{Path: p, Content: []byte("class A {}")}) {
			t.Errorf("%s should not be skipped", p)
		}
	}
	generated := "// <auto-generated>\n//     This code was generated by a tool.\n// </auto-generated>\nclass A {}\n"
	if !a.Skip(analysis.File{Path: "Src/Client.cs", Content: []byte(generated)}) {
		t.Error("a file with an <auto-generated> header should be skipped")
	}
}

func TestFrameworks(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []string
	}{
		{"aspnetcore and efcore", "using Microsoft.AspNetCore.Mvc;\nusing Microsoft.EntityFrameworkCore;\nusing System;\n", []string{"aspnetcore", "efcore"}},
		{"blazor is not counted as aspnetcore", "using Microsoft.AspNetCore.Components;\n", []string{"blazor"}},
		{"winforms is not counted as wpf", "using System.Windows.Forms;\nusing F = System.Windows.Forms.DataVisualization;\n", []string{"winforms"}},
		{"wpf", "using System.Windows;\nusing System.Windows.Controls;\n", []string{"wpf"}},
		{"maui and unity", "using Microsoft.Maui.Controls;\nusing UnityEngine;\n", []string{"maui", "unity"}},
		{"test frameworks, global and in a namespace", "global using Xunit;\nnamespace T {\n  using NUnit.Framework;\n  using Microsoft.VisualStudio.TestTools.UnitTesting;\n}\n", []string{"mstest", "nunit", "xunit"}},
		{"aliases name the target, not the alias", "using Xunit = MyLib.Assertions;\n", nil},
		{"none", "using System.Linq;\nclass A {}\n", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Analyzer{}.Frameworks(analysis.File{Path: "A.cs", Content: []byte(tc.src)})
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Frameworks = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRegistered(t *testing.T) {
	if a := analysis.ForPath("src/App/Program.cs"); a == nil || a.Language() != "csharp" {
		t.Errorf("ForPath(.cs) = %v, want the C# analyzer", a)
	}
	var _ analysis.FrameworkDetector = Analyzer{}
	var _ analysis.PathSkipper = Analyzer{}
}

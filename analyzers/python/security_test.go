package python

import (
	"reflect"
	"sort"
	"testing"

	"github.com/rburns629/urfluency/analysis"
)

func TestSecurityRules(t *testing.T) {
	src := `import os, pickle, subprocess, yaml, requests
def run(cursor: object, x: str, cmd: str, b: bytes) -> None:
    cursor.execute(f"SELECT * FROM t WHERE id = {x}")
    cursor.execute("SELECT * FROM t WHERE id = %s" % x)
    cursor.execute("SELECT {}".format(x))
    cursor.execute("SELECT * FROM t WHERE id = %s", (x,))
    eval(x)
    eval("1 + 1")
    pickle.loads(b)
    yaml.load(b)
    yaml.load(b, Loader=yaml.SafeLoader)
    subprocess.run(cmd, shell=True)
    subprocess.run(["ls", x])
    os.system("ls " + x)
    os.system("ls")
    requests.get(x, verify=False)
`
	want := []string{
		"py.eval-exec:7", "py.shell-injection:12", "py.shell-injection:14",
		"py.sql-string-built:3", "py.sql-string-built:4", "py.sql-string-built:5",
		"py.tls-verify-disabled:16", "py.unsafe-deserialization:10", "py.unsafe-deserialization:9",
	}
	if got := findings(t, "app/db.py", src); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v\nwant %v", got, want)
	}
}

func TestFrameworkRules(t *testing.T) {
	tests := []struct {
		name, path, src string
		want            []string
	}{
		{
			name: "fastapi untyped body and blocking calls",
			path: "app/api.py",
			src: `from fastapi import FastAPI
import time, requests
app = FastAPI()
@app.post("/items")
async def create(item: dict) -> dict:
    time.sleep(1)
    return item
@app.get("/items")
def list_items(q: dict) -> list:
    requests.get("https://example.com")
    return []
`,
			want: []string{"fastapi.blocking-call-in-async:6", "fastapi.untyped-body:5"},
		},
		{
			name: "django settings, csrf, and null text fields",
			path: "proj/settings.py",
			src: `from django.db import models
DEBUG = True
class M(models.Model):
    name = models.CharField(max_length=10, null=True)
    count = models.IntegerField(null=True)
@csrf_exempt
def view(request: object) -> None:
    DEBUG = True
`,
			want: []string{"django.csrf-exempt:6", "django.debug-true:2", "django.null-on-text-field:4"},
		},
		{
			name: "django queries per loop iteration",
			path: "shop/reports.py",
			src: `from django.db import models
from .models import Order, Customer
def totals(ids: list) -> list:
    out = []
    for i in ids:
        out.append(Order.objects.get(id=i))
    for o in Order.objects.filter(paid=True):
        out.append(o)
    names = [Customer.objects.get(id=i).name for i in ids]
    rows = [c for c in Customer.objects.all()]
    while out:
        Order.objects.filter(id=out.pop()).delete()
    return out + names + rows
`,
			// Line 2's relative import once crashed framework detection.
			want: []string{"django.query-in-loop:12", "django.query-in-loop:6", "django.query-in-loop:9"},
		},
		{
			name: "flask debug run",
			path: "app.py",
			src: `from flask import Flask
app = Flask(__name__)
app.run(debug=True)
`,
			want: []string{"flask.debug-mode:3"},
		},
		{
			name: "framework rules need the framework import",
			path: "app/other.py",
			src: `import time
async def f() -> None:
    time.sleep(1)
x = CharField(null=True)
`,
			want: []string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findings(t, tt.path, tt.src)
			sort.Strings(got)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFrameworkDetection(t *testing.T) {
	got := Analyzer{}.Frameworks(analysis.File{Path: "a.py", Content: []byte("import django.db\nfrom fastapi import FastAPI\nimport os\n")})
	sort.Strings(got)
	if want := []string{"django", "fastapi"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

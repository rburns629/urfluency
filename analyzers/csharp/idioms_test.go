package csharp

import (
	"context"
	"testing"

	"github.com/rburns629/urfluency/analysis"
	"github.com/rburns629/urfluency/rules"
)

func TestIdioms(t *testing.T) {
	src := `#nullable enable
using System.Linq;
using System.Threading;

namespace Shop;

public record Order(int Id, decimal Total);

public class Customer
{
    public required string Name { get; init; }
    public string? Nickname { get; set; }
    public int? Age { get; set; }
    public string Display => Nickname ?? Name;
}

public class OrderService
{
    public async Task<int> CountAsync(CancellationToken ct)
    {
        await using var conn = Open();
        using (var reader = conn.Read())
        {
            await reader.NextAsync(ct);
        }
        return 1;
    }

    async void OnClick(object sender, EventArgs e) => await CountAsync(default);

    public IEnumerable<int> Big(IEnumerable<Order> orders) =>
        orders.Where(o => o.Total > 100).OrderBy(o => o.Id).Select(o => o.Id);

    public IEnumerable<int> Query(IEnumerable<Order> orders) =>
        from o in orders where o.Total > 1 select o.Id;

    public string Describe(object o)
    {
        if (o is Order { Total: > 0 } order) { return order.Id.ToString(); }
        var count = orders.Count();
        return o switch { int n => "int", _ => "other" };
    }
}
`
	findings, err := Analyzer{}.Analyze(context.Background(), []analysis.File{{Path: "Shop/Orders.cs", Content: []byte(src)}})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	for _, f := range findings {
		if f.Positive {
			got[f.Rule]++
		}
	}
	want := map[string]int{
		"csharp.idiom.async-await": 1, "csharp.idiom.linq": 2, "csharp.idiom.using-disposal": 2,
		"csharp.idiom.records": 1, "csharp.idiom.pattern-matching": 2, "csharp.idiom.nullable-annotations": 2,
		"csharp.idiom.expression-bodied": 4, "csharp.idiom.init-only": 1, "csharp.idiom.cancellation-token": 1,
	}
	for rule, n := range want {
		if got[rule] != n {
			t.Errorf("%s = %d, want %d", rule, got[rule], n)
		}
	}
	for rule := range got {
		if r, ok := rules.Lookup(rule); !ok || r.Category != rules.Idiom {
			t.Errorf("%s is not an idiom in the catalog", rule)
		}
		if _, ok := want[rule]; !ok {
			t.Errorf("unexpected idiom %s = %d", rule, got[rule])
		}
	}
}

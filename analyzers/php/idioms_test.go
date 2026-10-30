package php

import (
	"context"
	"testing"

	"github.com/rburns629/urfluency/analysis"
	"github.com/rburns629/urfluency/rules"
)

func TestIdioms(t *testing.T) {
	src := `<?php
declare(strict_types=1);

namespace App;

enum Status: string
{
    case Active = 'active';
    case Banned = 'banned';
}

readonly class Money
{
    public function __construct(public int $amount, public string $currency) {}
}

final class UserService
{
    private ?Logger $logger = null;
    public readonly int $limit;
    private $legacy;

    public function __construct(
        private readonly UserRepository $users,
        private \PDO $pdo,
    ) {}

    public function label(Status $s): string
    {
        return match ($s) {
            Status::Active => 'ok',
            Status::Banned => 'no',
        };
    }

    public function find(int $id): ?string
    {
        $stmt = $this->pdo->prepare('SELECT name FROM users WHERE id = :id');
        $stmt->execute(['id' => $id]);
        $all = $this->pdo->prepare("SELECT * FROM users WHERE id = ?");
        $raw = $this->pdo->prepare("SELECT * FROM users WHERE id = $id");
        $this->logger?->info('found');
        $names = array_map(strtoupper(...), $this->users->names());
        $json = json_encode($names, flags: JSON_THROW_ON_ERROR);
        return $this->users->find($id)?->name;
    }
}

class Plain
{
    public function __construct($x) {}
}
`
	findings, err := Analyzer{}.Analyze(context.Background(), []analysis.File{{Path: "src/UserService.php", Content: []byte(src)}})
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
		"php.idiom.strict-types": 1, "php.idiom.enum": 1, "php.idiom.typed-property": 2,
		"php.idiom.constructor-promotion": 2, "php.idiom.readonly": 3, "php.idiom.match": 1,
		"php.idiom.nullsafe-operator": 2, "php.idiom.named-arguments": 1, "php.idiom.first-class-callable": 1,
		"php.idiom.prepared-statement": 2,
	}
	for rule, n := range want {
		if got[rule] != n {
			t.Errorf("%s = %d, want %d", rule, got[rule], n)
		}
	}
	for rule := range got {
		if _, ok := want[rule]; !ok {
			t.Errorf("unexpected idiom %s = %d", rule, got[rule])
		}
		if r, ok := rules.Lookup(rule); !ok || r.Category != rules.Idiom {
			t.Errorf("%s is not an idiom in the catalog", rule)
		}
	}
}

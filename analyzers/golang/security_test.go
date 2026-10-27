package golang

import (
	"reflect"
	"testing"
)

func TestSecurityRules(t *testing.T) {
	src := `package a
import (
	"context"
	"crypto/tls"
	"database/sql"
	"fmt"
	"os/exec"
)
func f(ctx context.Context, db *sql.DB, id, dir string) {
	db.Query(fmt.Sprintf("SELECT * FROM t WHERE id = %s", id))
	db.ExecContext(ctx, "DELETE FROM t WHERE id = " + id)
	db.Query("SELECT * FROM t WHERE id = $1", id)
	db.Query("SELECT 1" + " FROM t")
	exec.Command("sh", "-c", "ls " + dir)
	exec.Command("sh", "-c", "ls")
	exec.Command("ls", dir)
	_ = &tls.Config{InsecureSkipVerify: true}
}`
	want := []string{"go.shell-command:14", "go.sql-string-built:10", "go.sql-string-built:11", "go.tls-insecure-skip-verify:17"}
	if got := findings(t, "a.go", src); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

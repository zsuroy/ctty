package cmd

import (
	"strings"
	"testing"
)

func TestImportCommandRegistered(t *testing.T) {
	cmd, _, err := RootCmd.Find([]string{"import"})
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Name() != "import" {
		t.Errorf("got %q", cmd.Name())
	}
	if cmd.Flags().Lookup("from") == nil || cmd.Flags().Lookup("file") == nil || cmd.Flags().Lookup("dry-run") == nil {
		t.Fatal("expected --from, --file, --dry-run flags")
	}
	if !strings.Contains(cmd.Short, "Import SSH") {
		t.Errorf("short = %q", cmd.Short)
	}
}

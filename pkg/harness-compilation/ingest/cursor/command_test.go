package cursor

import "testing"

func TestLoadCommand_WorkspacePathNormalizesSource(t *testing.T) {
	cmd, err := LoadCommand(cursorWorkspaceFS, ".cursor/commands/my-cmd.md")
	if err != nil {
		t.Fatalf("LoadCommand(workspace): %v", err)
	}
	if cmd.Name() != "my-cmd" {
		t.Fatalf("Name() = %q, want %q", cmd.Name(), "my-cmd")
	}
	if cmd.Source.File != "commands/my-cmd.md" {
		t.Fatalf("Source.File = %q, want %q", cmd.Source.File, "commands/my-cmd.md")
	}
}

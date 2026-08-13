package harnesscompile

// Registration ownership: this root package owns the registration of every
// built-in compiler and ingester. Importing it is therefore sufficient — a
// consumer never needs a blank import of compile/<target> or ingest/<target>
// to make Compile, CompileBundle, Convert, IngestBundle, or Autodetect work.
//
// The per-target packages keep their init()-based self-registration so a single
// target can still be built and tested in isolation without pulling in the
// root package. None of them may import this package: that would create an
// import cycle. ValidateTargetRegistrations checks both directions of the
// declaration/registration boundary; target_registry_test.go enforces it.
import (
	_ "github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile/claude"
	_ "github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile/cursor"
	_ "github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile/devin"
	_ "github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile/factory"
	_ "github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile/gemini"
	_ "github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile/windsurf"

	_ "github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest/claude"
	_ "github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest/cursor"
	_ "github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest/devin"
	_ "github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest/factory"
	_ "github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest/gemini"
	_ "github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest/windsurf"
)

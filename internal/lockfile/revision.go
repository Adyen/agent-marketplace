package lockfile

const CurrentRevisionSchema = "v3"

type SourceRevisionContract struct {
	Schema                string
	Generator             string
	HookSupport           bool
	AdditiveWindsurfHooks bool
}

func RevisionContract(schema string) (SourceRevisionContract, error) {
	switch schema {
	case "v1":
		return SourceRevisionContract{Schema: "v1", Generator: "agent-marketplace source-revision/v1"}, nil
	case "v2":
		return SourceRevisionContract{Schema: "v2", Generator: "agent-marketplace source-revision/v2", HookSupport: true}, nil
	case "v3":
		return SourceRevisionContract{Schema: "v3", Generator: "agent-marketplace source-revision/v3", HookSupport: true, AdditiveWindsurfHooks: true}, nil
	default:
		return SourceRevisionContract{}, coded(CodeUnsupportedVersion, "unsupported source revision generator %q; use a CLI supporting this revision_schema or explicitly regenerate the lock with a supported generator", schema)
	}
}

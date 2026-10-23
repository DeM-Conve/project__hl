package config

// ---- Defaults for the config commands (version, schema, edit) ----
// Every default these commands use is named here, in one place, so it can move into a shared
// defaults package without hunting for literals.
const (
	// Version is the hl.yml format this hl writes and reads. Files without a `version:` line
	// are the older, unversioned format (0) and are upgraded the next time hl edits them.
	Version = 1

	// SchemaURL is the JSON Schema for hl.yml that editors use through the
	// yaml-language-server comment hl writes at the top of a new hl.yml.
	SchemaURL = "https://raw.githubusercontent.com/DeM-Conve/project__hl/main/schema/hl.schema.json"

	// DefaultEditor is what `hl edit` runs when neither $VISUAL nor $EDITOR is set.
	DefaultEditor = "vi"

	// EditDraft is the copy of hl.yml that `hl edit` opens, relative to the homelab folder.
	// hl.yml itself is only replaced once the draft is valid.
	EditDraft = ".hl/edit/" + FileName
)

// SchemaComment is the first line of a new hl.yml: it points editors at the schema.
const SchemaComment = "# yaml-language-server: $schema=" + SchemaURL

package remoteagent

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"path"
	"slices"
	"sort"
	"strings"

	"github.com/lovitus/dragfm-gui/internal/config"
	"github.com/lovitus/dragfm-gui/internal/transfer"
)

func validPartialPath(value string) bool {
	if !path.IsAbs(value) || path.Clean(value) != value {
		return false
	}
	base := path.Base(value)
	index := strings.LastIndex(base, ".dragfm-partial-")
	if index < 0 {
		return false
	}
	token := base[index+len(".dragfm-partial-"):]
	_, err := hex.DecodeString(token)
	return err == nil && len(token) >= 16 && len(token) <= 64
}

// Called under callMu. Save before replacing the in-memory record, so a vault
// failure cannot look like a successful pin or silently adopt a new inode.
func (s *Session) recordPartials(encoded string) error {
	var records []config.PartialRecord
	if encoded == "" || json.Unmarshal([]byte(encoded), &records) != nil {
		return transfer.PreserveSource(errors.New("helper did not return a valid partial ownership snapshot"))
	}
	seen := make(map[string]bool, len(records))
	for _, record := range records {
		if !validPartialPath(record.Path) || !numericIdentity(record.ParentID, 2) || (record.FileID != "" && !numericIdentity(record.FileID, 2)) || seen[record.Path] {
			return transfer.PreserveSource(errors.New("helper returned invalid or duplicate partial ownership metadata"))
		}
		seen[record.Path] = true
	}
	if slices.Equal(s.installation.Partials, records) {
		return nil
	}
	next := s.installation
	next.Partials = records
	if err := s.journal(next, false); err != nil {
		return transfer.PreserveSource(err)
	}
	s.installation = next
	return nil
}

// Must run while the installation's exclusive lease is held. Subshell cwd
// pins each original parent; final removal of the installation uses its own
// unchanged cwd. Validate every independent root before deleting any of them.
func partialCleanupScript(records []config.PartialRecord) (string, error) {
	ordered := append([]config.PartialRecord(nil), records...)
	sort.Slice(ordered, func(i, j int) bool { return len(ordered[i].Path) < len(ordered[j].Path) })
	var roots []config.PartialRecord
	seen := make(map[string]bool)
	for _, record := range ordered {
		if !validPartialPath(record.Path) || !numericIdentity(record.ParentID, 2) || (record.FileID != "" && !numericIdentity(record.FileID, 2)) || seen[record.Path] {
			return "", errors.New("invalid saved partial ownership metadata; retained")
		}
		seen[record.Path] = true
		covered := false
		for _, root := range roots {
			if strings.HasPrefix(record.Path, root.Path+"/") {
				covered = true
				break
			}
		}
		if !covered {
			roots = append(roots, record)
		}
	}
	var script strings.Builder
	for _, remove := range []bool{false, true} {
		for _, record := range roots {
			base := quotePOSIX("./" + path.Base(record.Path))
			script.WriteString("(\ncd -P -- " + quotePOSIX(path.Dir(record.Path)) + "\n")
			script.WriteString("test -x .\ntest \"$(command stat -Lc '%d:%i' -- .)\" = " + quotePOSIX(record.ParentID) + "\n")
			script.WriteString("if test -e " + base + " || test -L " + base + "; then\n")
			if record.FileID == "" {
				script.WriteString("printf '%s\\n' 'partial inode was not durably pinned; retained' >&2\nexit 74\n")
			} else {
				script.WriteString("test \"$(command stat -c '%d:%i' -- " + base + ")\" = " + quotePOSIX(record.FileID) + "\n")
				if remove {
					// Do not restore permissions through shell pathname-based
					// chmod: it may dereference a raced symlink. If the admitted
					// identity cannot remove this tree, keep the record and error;
					// only an explicitly approved root record runs as root.
					script.WriteString("command rm -rf --one-file-system -- " + base + "\n")
				}
			}
			script.WriteString("fi\n)\n")
		}
	}
	return script.String(), nil
}

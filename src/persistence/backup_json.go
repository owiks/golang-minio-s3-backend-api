package persistence

import (
	"encoding/json"
	"fmt"
	"io"
)

// WriteJSON emits one bounded record at a time. In particular a response with
// deeply nested JSON is not expanded by pretty-printing the entire snapshot.
// The caller enforces the total byte budget on its writer.
func (b Backup) WriteJSON(w io.Writer) error {
	if _, err := fmt.Fprintf(w, "{\n  \"version\": %d,\n  \"connections\": ", b.Version); err != nil {
		return err
	}
	if err := writeBackupArray(w, b.Connections); err != nil {
		return err
	}
	if _, err := io.WriteString(w, ",\n  \"accesses\": "); err != nil {
		return err
	}
	if err := writeBackupArray(w, b.Accesses); err != nil {
		return err
	}
	if _, err := io.WriteString(w, ",\n  \"events\": "); err != nil {
		return err
	}
	if err := writeBackupArray(w, b.Events); err != nil {
		return err
	}
	if _, err := io.WriteString(w, ",\n  \"commands\": "); err != nil {
		return err
	}
	if err := writeBackupArray(w, b.Commands); err != nil {
		return err
	}
	_, err := io.WriteString(w, "\n}\n")
	return err
}

func writeBackupArray[T any](w io.Writer, values []T) error {
	if values == nil {
		_, err := io.WriteString(w, "null")
		return err
	}
	if _, err := io.WriteString(w, "["); err != nil {
		return err
	}
	encoder := json.NewEncoder(w)
	for i, value := range values {
		if i > 0 {
			if _, err := io.WriteString(w, ","); err != nil {
				return err
			}
		}
		if _, err := io.WriteString(w, "\n    "); err != nil {
			return err
		}
		if err := encoder.Encode(value); err != nil {
			return err
		}
	}
	_, err := io.WriteString(w, "  ]")
	return err
}

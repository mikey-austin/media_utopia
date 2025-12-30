package output

import (
	"encoding/json"
	"fmt"
	"os"
)

// JSONPrinter prints JSON to stdout.
type JSONPrinter struct{}

// Print renders JSON output.
func (JSONPrinter) Print(v any) error {
	payload, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(os.Stdout, string(payload))
	return err
}

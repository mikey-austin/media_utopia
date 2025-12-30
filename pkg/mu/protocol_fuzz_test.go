package mu

import "testing"

func FuzzValidateCommandEnvelope(f *testing.F) {
	f.Add("id", "type", int64(1), "from", "{}")
	f.Add("", "", int64(0), "", "")

	f.Fuzz(func(t *testing.T, id string, typ string, ts int64, from string, body string) {
		cmd := CommandEnvelope{
			ID:   id,
			Type: typ,
			TS:   ts,
			From: from,
			Body: []byte(body),
		}
		_ = ValidateCommandEnvelope(cmd)
	})
}

package config

import "errors"

// ValidateBasic checks the captured account only for HTTP serving. Local
// probe and migration commands do not require a serving account.
func (c Config) ValidateBasic() error {
	if len(c.BasicAuthUsername) < 1 || len(c.BasicAuthUsername) > 128 {
		return errors.New("BASIC_AUTH_USERNAME must contain 1–128 ASCII bytes from ! through ~ excluding colon")
	}
	for i := range len(c.BasicAuthUsername) {
		b := c.BasicAuthUsername[i]
		if b < '!' || b > '~' || b == ':' {
			return errors.New("BASIC_AUTH_USERNAME must contain 1–128 ASCII bytes from ! through ~ excluding colon")
		}
	}
	if len(c.BasicAuthPassword) < 1 || len(c.BasicAuthPassword) > 256 {
		return errors.New("BASIC_AUTH_PASSWORD must contain 1–256 printable ASCII bytes")
	}
	for i := range len(c.BasicAuthPassword) {
		if b := c.BasicAuthPassword[i]; b < ' ' || b > '~' {
			return errors.New("BASIC_AUTH_PASSWORD must contain 1–256 printable ASCII bytes")
		}
	}
	return nil
}

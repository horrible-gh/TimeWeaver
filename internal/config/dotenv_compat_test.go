package config

import "testing"

func TestQuotedValuesMatchPythonDotenv(t *testing.T) {
	// Expected values were checked against python-dotenv without importing the app.
	cases := map[string]string{
		`VALUE="literal\x41\u0062"`:          `literal\x41\u0062`,
		`VALUE="D:\work\project.db"`:         `D:\work\project.db`,
		`VALUE='D:\work\timeweaver.sqlite3'`: `D:\work\timeweaver.sqlite3`,
		`VALUE="a\'b"`:                       `a'b`,
		`VALUE="quote\" and tab\t"`:          "quote\" and tab\t",
		`VALUE= # comment`:                   `# comment`,
	}
	for input, want := range cases {
		values, err := parseEnv(input)
		if err != nil {
			t.Fatal(err)
		}
		if values["VALUE"] != want {
			t.Errorf("dotenv value changed for %q: got %q want %q", input, values["VALUE"], want)
		}
	}
}

package config

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseIni(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    IniFile
		wantErr bool
	}{
		{
			name: "basic sections",
			input: `
[section1]
key1 = value1
key2 = value2

[section2]
key3 = value3
`,
			want: IniFile{
				"section1": {"key1": "value1", "key2": "value2"},
				"section2": {"key3": "value3"},
			},
			wantErr: false,
		},
		{
			name: "global section",
			input: `
globalKey = globalValue
[section1]
key1 = value1
`,
			want: IniFile{
				"":         {"globalKey": "globalValue"},
				"section1": {"key1": "value1"},
			},
			wantErr: false,
		},
		{
			name: "comments and whitespace",
			input: `
# This is a comment
; Another comment

[section1]
  key1  =  value1  
	key2=value2
`,
			want: IniFile{
				"section1": {"key1": "value1", "key2": "value2"},
			},
			wantErr: false,
		},
		{
			name: "empty values",
			input: `
[section1]
key1 =
key2
`,
			want: IniFile{
				"section1": {"key1": "", "key2": ""},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := strings.NewReader(tt.input)
			got, err := ParseIni(reader)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseIni() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseIni() = %v, want %v", got, tt.want)
			}
		})
	}
}

package builder

import (
	"bytes"
	"log"
	"testing"

	dockercmd "github.com/docker/docker/builder/command"
	"github.com/docker/docker/builder/parser"
)

func TestReplaceValidCmd(t *testing.T) {
	tests := []struct {
		name           string
		cmd            string
		replaceArgs    string
		fileData       []byte
		edited         bool
		expectedOutput string
		expectedErr    error
	}{
		{
			name:           "from-replacement",
			cmd:            dockercmd.From,
			replaceArgs:    "other/image",
			fileData:       []byte(dockerFile),
			edited:         true,
			expectedOutput: expectedFROM,
			expectedErr:    nil,
		},
		{
			name:           "run-replacement",
			cmd:            dockercmd.Run,
			replaceArgs:    "This test kind-of-fails before string replacement so this string won't be used",
			fileData:       []byte(dockerFile),
			edited:         false,
			expectedOutput: "",
			expectedErr:    replaceCmdErr,
		},
		{
			name:           "invalid-dockerfile-cmd",
			cmd:            "blabla",
			replaceArgs:    "This test fails at start so this string won't be used",
			fileData:       []byte(dockerFile),
			edited:         false,
			expectedOutput: "",
			expectedErr:    invalidCmdErr,
		},
		{
			name:           "no-cmd-in-dockerfile",
			cmd:            dockercmd.Cmd,
			replaceArgs:    "runme.sh",
			fileData:       []byte(dockerFile),
			edited:         false,
			expectedOutput: "",
			expectedErr:    replaceCmdErr,
		},
		{
			name:           "trailing-slash",
			cmd:            dockercmd.From,
			replaceArgs:    "rhel",
			fileData:       []byte(trSlashFile),
			edited:         true,
			expectedOutput: expectedtrSlashFile,
			expectedErr:    nil,
		},
		{
			name:           "multiple trailing slashes plus plus",
			cmd:            dockercmd.From,
			replaceArgs:    "scratch",
			fileData:       []byte(trickierFile),
			edited:         true,
			expectedOutput: expectedTrickierFile,
			expectedErr:    nil,
		},
	}

	for _, test := range tests {
		out, err := replaceValidCmd(test.cmd, test.replaceArgs, test.fileData)
		if err != test.expectedErr {
			t.Errorf("%s: Unexpected error: Expected %v, got %v", test.name, test.expectedErr, err)
		}
		if out != test.expectedOutput {
			t.Errorf("%s: Unexpected output:\n\nExpected:\n%s\n(length: %d)\n\ngot:\n%s\n(length: %d)",
				test.name, test.expectedOutput, len(test.expectedOutput), out, len(out))
		}
	}

	// Re-use the tests above
	var buf *bytes.Buffer
	for _, test := range tests {
		buf = bytes.NewBuffer([]byte(test.fileData))
		original, err := parser.Parse(buf)
		if err != nil {
			log.Println(err)
		}
		repl, err := replaceValidCmd(test.cmd, test.replaceArgs, test.fileData)
		if err != nil {
			log.Println(err)
		}
		buf = bytes.NewBuffer([]byte(repl))
		edited, err := parser.Parse(buf)
		if err != nil {
			log.Println(err)
		}

		diff := cmpASTs(original, edited)

		// Note that these tests will probably fail for Dockerfiles where we have replaced
		// multiline command arguments
		if test.edited && diff != 1 {
			t.Errorf("%s: Edit mismatch, expected one edit, got %d", test.name, diff)
		} else if !test.edited && diff > 0 {
			t.Errorf("%s: Edit mismatch, expected no edit, got %d", test.name, diff)
		}
	}
}

// cmpASTs compares two Abstract Syntax Trees and returns the
// amount of differences between them
func cmpASTs(original *parser.Node, edited *parser.Node) int {
	index := 0
	if original.Value != edited.Value {
		index++
	}

	originalChildren := make([]*parser.Node, 0)
	for _, n := range original.Children {
		originalChildren = append(originalChildren, n)
	}
	editedChildren := make([]*parser.Node, 0)
	for _, n := range edited.Children {
		editedChildren = append(editedChildren, n)
	}
	for i := 0; i < len(editedChildren); i++ {
		index += cmpASTs(originalChildren[i], editedChildren[i])
	}

	if original.Next != nil && edited.Next != nil {
		index += cmpASTs(original.Next, edited.Next)
	} else if original.Next != edited.Next {
		index++
	}
	return index
}

func TestTraverseAST(t *testing.T) {
	tests := []struct {
		name     string
		cmd      string
		fileData []byte
		expected int
	}{
		{
			name:     "dockerFile",
			cmd:      dockercmd.Entrypoint,
			fileData: []byte(dockerFile),
			expected: 1,
		},
		{
			name:     "expectedFROM",
			cmd:      dockercmd.From,
			fileData: []byte(expectedFROM),
			expected: 2,
		},
		{
			name:     "trSlashFile",
			cmd:      dockercmd.Entrypoint,
			fileData: []byte(trSlashFile),
			expected: 0,
		},
		{
			name:     "expectedtrSlashFile",
			cmd:      dockercmd.Cmd,
			fileData: []byte(expectedtrSlashFile),
			expected: 1,
		},
	}

	var buf *bytes.Buffer
	for _, test := range tests {
		buf = bytes.NewBuffer([]byte(test.fileData))
		node, err := parser.Parse(buf)
		if err != nil {
			log.Println(err)
		}

		howMany := traverseAST(test.cmd, node)
		if howMany != test.expected {
			t.Errorf("Wrong result, expected %d, got %d", test.expected, howMany)
		}
	}
}

const (
	dockerFile = `
FROM openshift/origin-base
FROM candidate

RUN mkdir -p /var/lib/openshift

ADD bin/openshift        /usr/bin/openshift
RUN ln -s /usr/bin/openshift /usr/bin/osc && \

ENV HOME /root
WORKDIR /var/lib/openshift
ENTRYPOINT ["/usr/bin/openshift"]
`

	expectedFROM = `
FROM openshift/origin-base
FROM other/image

RUN mkdir -p /var/lib/openshift

ADD bin/openshift        /usr/bin/openshift
RUN ln -s /usr/bin/openshift /usr/bin/osc && \

ENV HOME /root
WORKDIR /var/lib/openshift
ENTRYPOINT ["/usr/bin/openshift"]
`

	trSlashFile = `
from \
centos
CMD "cat /etc/passwd"
`
	expectedtrSlashFile = `
from \
rhel
CMD "cat /etc/passwd"
`

	trickierFile = `
from centos \
rhel \
ubuntu

CMD ["executable","param1","param2"]
`

	expectedTrickierFile = `
from \
scratch

CMD ["executable","param1","param2"]
`
)

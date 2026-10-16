package modules

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDefault_UniqueNamesAndFiles(t *testing.T) {
	names, files := map[string]bool{}, map[string]bool{}
	for _, m := range Default(Settings{}).All() {
		assert.False(t, names[m.Name()], "duplicate module name %s", m.Name())
		assert.False(t, files[m.File()], "duplicate policy file %s", m.File())
		names[m.Name()], files[m.File()] = true, true
	}
}

package subset_test

import (
	"github.com/guoyyit/font-raster-subset-go/internal/sfntwrite"
)

func assembleForTest(tables map[string][]byte) []byte {
	return sfntwrite.Assemble(tables)
}

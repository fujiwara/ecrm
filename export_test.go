package ecrm

var (
	ReadExcludeFile = readExcludeFile
)

func NewImagesSet() map[string]set {
	return make(map[string]set)
}

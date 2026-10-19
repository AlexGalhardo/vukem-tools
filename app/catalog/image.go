package catalog

// imageCatalog is the raster images: convert, compress, resize, crop, rotate
// domain (S4.1 to S4.3).
//
// A catalogue entry is written when the tool is planned, not when it ships:
// Status says which. See the package comment for why a missing tool is listed
// rather than silently absent.
//
// It is empty, and that is deliberate rather than unfinished. The tool this
// domain ships is `image-convert`, whose entry is already in catalog.go, in
// the slice named pixelCatalog — that slice holds the whole original
// catalogue rather than only the pixel-art domain. Repeating the entry here
// would list the tool twice on the index and make Find return whichever came
// first. Moving it into this slice is a change to catalog.go, which this
// domain does not own; see the report that accompanies this branch.
var imageCatalog = []Tool{
	{
		Name:     "image-convert",
		Title:    "Convert an image",
		Summary:  "Between PNG, JPEG, WebP, AVIF and more, with control over quality and whether transparency is kept.",
		In:       []string{"PNG", "JPEG", "WebP", "AVIF", "GIF", "TIFF", "BMP"},
		Out:      []string{"PNG", "JPEG", "WebP", "AVIF"},
		Group:    GroupImages,
		Icon:     "image",
		Status:   Live,
		Keywords: []string{"png", "jpg", "jpeg", "webp", "avif", "heic", "resize"},
	},
	{
		Name:     "image-compress",
		Title:    "Compress an image",
		Summary:  "Makes an image smaller without a visible difference, and reports exactly how much was saved.",
		In:       []string{"PNG", "JPEG", "WebP", "GIF"},
		Out:      []string{"PNG", "JPEG", "WebP", "GIF"},
		Group:    GroupImages,
		Icon:     "compress",
		Status:   Live,
		Keywords: []string{"optimise", "optimize", "smaller", "lossless", "tinypng", "shrink"},
	},
}

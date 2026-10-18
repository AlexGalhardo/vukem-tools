package catalog

// imageCatalog is the raster images: convert, compress, resize, crop, rotate domain (S4.1 to S4.3).
//
// A catalogue entry is written when the tool is planned, not when it ships:
// Status says which. See the package comment for why a missing tool is listed
// rather than silently absent.
var imageCatalog = []Tool{}

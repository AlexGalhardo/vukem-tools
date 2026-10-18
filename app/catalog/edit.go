package catalog

// editCatalog is the cutting, cropping and compression domain (S3.2 and S3.3).
//
// A catalogue entry is written when the tool is planned, not when it ships:
// Status says which. See the package comment for why a missing tool is listed
// rather than silently absent.
var editCatalog = []Tool{}

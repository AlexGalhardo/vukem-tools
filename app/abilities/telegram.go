package abilities

// Telegram is what a key made for the Telegram bot may do: exactly the tools
// its menus offer, and nothing else.
//
// It lives here, beside the abilities themselves, because two places need the
// same list and neither may own it. The server grants it when a person
// approves the bot (app/devicelink), so a bot that asked for more would not
// get it; the bot's tool table is checked against it by a test, so a tool
// added to a menu without widening the grant, or a grant widened for a tool
// no menu offers, fails the build.
func Telegram() []string {
	return []string{
		ImageConvert,
		ImageCompress,
		PixelsSVG,
		AudioConvert,
		VideoConvert,
		VideoEdit,
		VideoDownload,
		PDFCompress,
		PDFConvert,
		PDFStructure,
		Archive,
	}
}

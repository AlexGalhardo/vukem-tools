# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.6.1] - 2026-10-24

### Fixed

- **ui:** Stack the theme options with the icon beside its name ([c758159](https://github.com/AlexGalhardo/vukem-tools/commit/c7581592ee3bdc7a169c28ea45ed8130a2b0ab99))

## [0.6.0] - 2026-10-24

### Added

- **ui:** Add a sidebar with grouped tools, Ctrl+K search and the developer tools ([f02060e](https://github.com/AlexGalhardo/vukem-tools/commit/f02060e9f020dad5ef28c9676d9679d8bc85f673))
- **pdf:** Convert Office documents, recognise scans, fill forms and sign with a certificate ([19f104c](https://github.com/AlexGalhardo/vukem-tools/commit/19f104c55796f0429dfe200556318fec942936f8))
- **bot:** Sign each person in from the chat and serve the tools from button menus ([d4772e4](https://github.com/AlexGalhardo/vukem-tools/commit/d4772e4ed5f0cffa91cf9e0cf0562b0ba8f3f09b))

### Fixed

- **infra:** Name the signal when an engine is killed ([73c529c](https://github.com/AlexGalhardo/vukem-tools/commit/73c529cccf1e66dacaae80bdd1c4a6eb2e9d3769))
- **deps:** Override markdown-it past two quadratic-time advisories ([76a2104](https://github.com/AlexGalhardo/vukem-tools/commit/76a2104c3676fd674754898bd0edcaefb50b2053))
- **infra:** Build on Go 1.27.3 for the crypto/x509 advisory GO-2026-6809 ([c838452](https://github.com/AlexGalhardo/vukem-tools/commit/c838452ac2123244e1872a70a0fe551cf6dec553))
- **infra:** Stop an engine probe from waiting on a child that holds its pipe ([49de10e](https://github.com/AlexGalhardo/vukem-tools/commit/49de10e9e7ca6fc51f99547807c653e432eaaea0))

## [0.5.0] - 2026-10-19

### Added

- **ui:** Serve /sitemap.xml ([9c60315](https://github.com/AlexGalhardo/vukem-tools/commit/9c60315ba99e5007400eb50a58f584aed7ba9510))

### Changed

- **ui:** Trim the header to what it is for ([a6e1c04](https://github.com/AlexGalhardo/vukem-tools/commit/a6e1c04bac59724230f85c993babf88e27037bc8))
- **ui:** Drop the header links that duplicate what is already there ([e4a5ed7](https://github.com/AlexGalhardo/vukem-tools/commit/e4a5ed7cbb505599f5a50cf796330e6d60ffd1fc))

### Fixed

- **infra:** Size the engine process limit from what the UID already owns ([6ea47c1](https://github.com/AlexGalhardo/vukem-tools/commit/6ea47c1e4ce770e4ca6c75288f59acd1fb2d6f24))
- **image:** Keep the cause when the engine printed nothing ([09cf93c](https://github.com/AlexGalhardo/vukem-tools/commit/09cf93c2031b4bca0fdb4ba82298ac7762b0ccfa))
- **jobs:** Resolve the job working directory absolutely ([1074b84](https://github.com/AlexGalhardo/vukem-tools/commit/1074b84a58994a94996a3773ffac237d0c3bdcd7))
- **ui:** Leave the Inertia app to reach the API reference ([dcc02b4](https://github.com/AlexGalhardo/vukem-tools/commit/dcc02b4b26190444a55a274417d11706fc5ff926))

### Security

- **repo:** Close the five stale vukem workarounds ([c3e896b](https://github.com/AlexGalhardo/vukem-tools/commit/c3e896bf770d2106371951f77d199c4068a51b0a))

## [0.4.0] - 2026-10-19

### Added

- **sprite:** Write the five atlas formats a game engine imports ([ef48497](https://github.com/AlexGalhardo/vukem-tools/commit/ef48497ddb598468494b9609782bb0615222a9c9))
- **sprite:** Ship sprite-split and sprite-build ([9683835](https://github.com/AlexGalhardo/vukem-tools/commit/9683835209e400178556f8c9ebf547c2dc682d5c))
- **ui:** Draw the grid over the sheet, and show the packed result ([a760ebe](https://github.com/AlexGalhardo/vukem-tools/commit/a760ebe69a4c0ac30481d3205ddbad7b126c5f58))

### Fixed

- **jobs:** Give a conversion somewhere to say what it did ([af38a46](https://github.com/AlexGalhardo/vukem-tools/commit/af38a469247d45974544e787749cab5b827e4488))
- **ui:** Allow blob: images, so a dropped file can be previewed ([ca21f29](https://github.com/AlexGalhardo/vukem-tools/commit/ca21f2905b6590aac69e2083a016025a455f7c5c))

## [0.3.0] - 2026-10-19

### Added

- **ui:** Rebuild the header for a phone ([4e5601c](https://github.com/AlexGalhardo/vukem-tools/commit/4e5601c3d53d1a8d23109d6b5500009a0db14457))
- **image:** Compress an image without changing what it is ([0bdab01](https://github.com/AlexGalhardo/vukem-tools/commit/0bdab0137f067793abe9b73562688d886ac5d3ee))

### Fixed

- **image:** Turn on PNG row filters, which were off on every PNG written ([5f6ba8e](https://github.com/AlexGalhardo/vukem-tools/commit/5f6ba8e1deeac9bab1104d57c617660eba366f11))

## [0.2.0] - 2026-10-19

### Added

- **archive:** Create and extract ZIP, TAR, TAR.GZ and GZ ([9910ab2](https://github.com/AlexGalhardo/vukem-tools/commit/9910ab2d2ccbf7ad80908abfcd42b3e0564c01fe))

### Changed

- **ui:** Lift the generic tool form out of the pdf domain ([2c72fba](https://github.com/AlexGalhardo/vukem-tools/commit/2c72fbaf2abd957d7afb73150a8329cc172d15c9))

### Fixed

- **api:** Authenticate the app's own pages, render every PDF page, show the new key ([7b3023d](https://github.com/AlexGalhardo/vukem-tools/commit/7b3023df67fc100dd496ca1169794ae1181f5e17))
- **infra:** Resolve the engine probe directory per boot and absolutely ([cd2b97f](https://github.com/AlexGalhardo/vukem-tools/commit/cd2b97fb3cde97f3011776aca098f4668dff983d))
- **jobs:** Keep a failure's specific message on the conversion ([c88c07c](https://github.com/AlexGalhardo/vukem-tools/commit/c88c07c94349339322805c2bc040ffdb47133042))

## [0.1.0] - 2026-10-19

### Added

- **repo:** Scaffold the vukem-tools foundation on vukem v0.28.0 ([7e7bfd0](https://github.com/AlexGalhardo/vukem-tools/commit/7e7bfd00b21f227d2941ccbc7701bb06c267c8a1))
- **infra:** Run on postgres in every environment ([3047887](https://github.com/AlexGalhardo/vukem-tools/commit/304788750a7cad673274efd5447ae33a7c669f30))
- **jobs:** Add the conversions, files and usage counter tables ([a0fe677](https://github.com/AlexGalhardo/vukem-tools/commit/a0fe677a8d8374b03e91ee2e550f0a43c06f6de3))
- **apikeys:** Scope every API key to the tools it may use ([dd28448](https://github.com/AlexGalhardo/vukem-tools/commit/dd28448c260d1e7a93ef472bfe879ef8a2596bd3))
- **infra:** Report every dependency on the deep probe and name the deploy ([0604b5c](https://github.com/AlexGalhardo/vukem-tools/commit/0604b5cbbbcd70892aacc3c34949c5da2303387c))
- **auth:** Enforce the password policy and show it while typing ([10705d7](https://github.com/AlexGalhardo/vukem-tools/commit/10705d7a65b571fd121a6b1d74831f819d90d9f7))
- **auth:** Sign in with Google ([856fb5c](https://github.com/AlexGalhardo/vukem-tools/commit/856fb5cbdc7c8bdd3e4ea054b04ee1205c1f14a5))
- **api:** Generate the OpenAPI document and serve the Scalar reference ([6087cdf](https://github.com/AlexGalhardo/vukem-tools/commit/6087cdfd4abeb269b539906e20798f50afa06006))
- **pixelsvg:** Add the pixels-to-svg conversion library ([d53feff](https://github.com/AlexGalhardo/vukem-tools/commit/d53feff90267aed48dc8ff762cd6b2b2b3f838c6))
- **jobs:** Generate sortable conversion ids ([0b20a12](https://github.com/AlexGalhardo/vukem-tools/commit/0b20a12e95d5a4e2c02506e5517fe72191f629eb))
- **jobs:** Define the conversion state machine and error taxonomy ([847708f](https://github.com/AlexGalhardo/vukem-tools/commit/847708f43f68fe3135bf2491d003a66f1b499854))
- **jobs:** Carry progress between processes over LISTEN/NOTIFY ([cadb3ab](https://github.com/AlexGalhardo/vukem-tools/commit/cadb3abdebb7d917cfad36a24474b4089a4fc364))
- **jobs:** Run conversions through the queue with progress and cancellation ([4716c15](https://github.com/AlexGalhardo/vukem-tools/commit/4716c1525bf29d23ff00b26d95c99ebef31fe714))
- **jobs:** Expose conversions over the API with a progress stream ([d50c086](https://github.com/AlexGalhardo/vukem-tools/commit/d50c08693ef9a2b7ff25d9b7cae94bebf38a9745))
- **upload:** Identify files by their bytes, not their name ([2a301d7](https://github.com/AlexGalhardo/vukem-tools/commit/2a301d7d1ef318b7903eb36c9700ea730e8231d0))
- **storage:** Keep results in the bucket behind expiring signed links ([d06a70d](https://github.com/AlexGalhardo/vukem-tools/commit/d06a70de6f02904fa295f9826d562d02058f8a8d))
- **upload:** Accept resumable uploads, scan them, and serve expiring downloads ([193e932](https://github.com/AlexGalhardo/vukem-tools/commit/193e932dec1ac8b91bb6adcfa24e880fd8c1c69d))
- **pixelsvg:** Submit a conversion and run it end to end ([b9c6a1e](https://github.com/AlexGalhardo/vukem-tools/commit/b9c6a1e3425a3fd693d7cfa90df36fcce228f6b9))
- **api:** Give each tool its own typed submit route ([893f356](https://github.com/AlexGalhardo/vukem-tools/commit/893f35675dd2854099b0bcb71409725f48755c33))
- **jobs:** Meter processing time against a daily budget per account ([b9a640c](https://github.com/AlexGalhardo/vukem-tools/commit/b9a640c41cc546ad49922fdbea384a934ef17a67))
- **ui:** Build the shell as an instrument, not a landing page ([e7bfa2b](https://github.com/AlexGalhardo/vukem-tools/commit/e7bfa2b676aab5bac701447dd922a70abdda17ca))
- **ui:** Upload, watch and download a conversion ([d87f0f7](https://github.com/AlexGalhardo/vukem-tools/commit/d87f0f7e2fcd86b7e1280c119ee1242fb3771a01))
- **infra:** Run every engine as a limited subprocess in a pinned container ([b1752db](https://github.com/AlexGalhardo/vukem-tools/commit/b1752dbe7001a53002022de23f8e229d2e6f1b75))
- **infra:** Separate the liveness probe from the dependency check ([16f9a98](https://github.com/AlexGalhardo/vukem-tools/commit/16f9a986f06f4dad1c27d345b89af7c501a9ceac))
- **ui:** Add the account area ([31bc883](https://github.com/AlexGalhardo/vukem-tools/commit/31bc8837f2978a6b3c6557fd5c0c64e85130609c))
- **image:** Add the libvips conversion and edit driver ([2ea6f31](https://github.com/AlexGalhardo/vukem-tools/commit/2ea6f31dfa80fb4ad408e39a32996997215a05df))
- **video:** Add the ffmpeg conversion drivers for video and audio ([41488aa](https://github.com/AlexGalhardo/vukem-tools/commit/41488aadc48941dcec5dab7e4b1216c2d72cf705))
- **api:** Add the image, video and audio tools, routes and capability endpoints ([f1bf76a](https://github.com/AlexGalhardo/vukem-tools/commit/f1bf76ae60e3eca312a012f83488fc33e2376547))
- **ui:** Add the image, video, audio and GIF tool pages ([1363af1](https://github.com/AlexGalhardo/vukem-tools/commit/1363af1150c8bcef25ad6a66d71bc24d6d50dc3b))
- **repo:** Merge the s4 image, video and audio tools ([790201c](https://github.com/AlexGalhardo/vukem-tools/commit/790201cb6a9299b43e4d8fbb9ac7f3ed3c47f1bc))
- **pdf:** Publish the PDF catalogue with every decision and its reason ([a06fe9a](https://github.com/AlexGalhardo/vukem-tools/commit/a06fe9a48638c774c32d077e90ad1ae6a9e8c527))
- **api:** Add a typed submit route for each PDF tool ([6e1706e](https://github.com/AlexGalhardo/vukem-tools/commit/6e1706eb40ae6bffaa08dcb028d9194f56fed55e))
- **ui:** Add one data-driven page for every PDF tool ([8cfaee4](https://github.com/AlexGalhardo/vukem-tools/commit/8cfaee46fc9e67ac424df7f25aeec81104446c50))
- **repo:** Merge the s6 pdf tools ([a251034](https://github.com/AlexGalhardo/vukem-tools/commit/a251034ce5aad16cdb8b6e3e4c386c50244df55c))
- **downloader:** Drive yt-dlp behind a closed platform list ([9a35aa6](https://github.com/AlexGalhardo/vukem-tools/commit/9a35aa6acab41cc0cead78b3f4cb97b99c47754e))
- **editor:** Trim, crop and split video through ffmpeg and ffprobe ([34ade9c](https://github.com/AlexGalhardo/vukem-tools/commit/34ade9c1d6eee7de0015f10b4ffbbfb78329939c))
- **api:** Expose downloading, cutting and compressing over the API ([9e8473c](https://github.com/AlexGalhardo/vukem-tools/commit/9e8473cf7c5c08e367eb65880f95882c1dba1a5d))
- **bot:** Add the Telegram front end as its own binary ([0f2081a](https://github.com/AlexGalhardo/vukem-tools/commit/0f2081a633e2249bd7e850376ac0046f96b96eb0))
- **ui:** Add the video download page ([8464ed1](https://github.com/AlexGalhardo/vukem-tools/commit/8464ed13c7b9c164b95b61f5a045d83f395de784))
- **ui:** Add the cut, crop and compress pages ([52cc67a](https://github.com/AlexGalhardo/vukem-tools/commit/52cc67a513d00ce3c7dcfca203a8b862759d20e8))
- **repo:** Merge the s3 download, cut and compress tools ([920df2d](https://github.com/AlexGalhardo/vukem-tools/commit/920df2ddcd58eddfe984dbd47908cf6c4bd4dd06))
- **sprite:** Split a sheet into frames with an atlas description ([0ef89e8](https://github.com/AlexGalhardo/vukem-tools/commit/0ef89e8cafecd46f314f5af9dd40f2a390b065bd))
- **sprite:** Build a sheet from an archive of frames ([07d5ff4](https://github.com/AlexGalhardo/vukem-tools/commit/07d5ff4945a4457f1ec30b02014e41016b816350))
- **repo:** Merge the s5 sprite sheet groundwork ([28ea557](https://github.com/AlexGalhardo/vukem-tools/commit/28ea557819599e51190c489d662662802ca89266))
- **repo:** Register the media and pdf tools and land the shared seams ([cf211e8](https://github.com/AlexGalhardo/vukem-tools/commit/cf211e85f6e9454c506e4e24772ad8e3d4b9eb76))
- **auth:** Drop email confirmation as a gate and probe outside WORK_DIR ([5588bb4](https://github.com/AlexGalhardo/vukem-tools/commit/5588bb4793bd42f8090f15cb332516049119bafb))
- **auth:** Put password sign-in behind PASSWORD_AUTH, leaving Google ([642610e](https://github.com/AlexGalhardo/vukem-tools/commit/642610e7ef3c970ebffa6b618e68ea8f0999c3fd))

### Changed

- **infra:** Read the container CPU quota in one place ([08ebce9](https://github.com/AlexGalhardo/vukem-tools/commit/08ebce9505750c0961aa0e6ac8bcc797ec6e757f))
- **repo:** Register tools and catalogue entries per domain ([0095342](https://github.com/AlexGalhardo/vukem-tools/commit/00953427e4c76ec68787baf53a549a71ee173c95))
- **pdf:** Record every PDF path's figures from the production image ([5a48f22](https://github.com/AlexGalhardo/vukem-tools/commit/5a48f228f823d3ce8b7272b99920fac06efb8db2))

### Fixed

- **ui:** Title the pages after the app, not after the framework ([91fee25](https://github.com/AlexGalhardo/vukem-tools/commit/91fee2555b982ff907462778fae6d675d8c2718b))
- **ui:** Count password length the way the server counts it ([9b6f100](https://github.com/AlexGalhardo/vukem-tools/commit/9b6f1000678fbd55e349ef3f4f51d7daa3c4df85))
- **infra:** Read an engine's version from stderr too ([3585e75](https://github.com/AlexGalhardo/vukem-tools/commit/3585e75d0da0b1f0fecb55f3e201bb81c10839ef))
- **BREAKING:** **video:** Constrain every value spliced into an engine sub-language ([49be6f4](https://github.com/AlexGalhardo/vukem-tools/commit/49be6f458ce72ba560856b75b9db85acc05c5bc7))
- **video:** Bound SVT-AV1's own thread pool and stop blaming the file for it ([3ff5f35](https://github.com/AlexGalhardo/vukem-tools/commit/3ff5f35ea8c078ea0f4775c7a9690f16face6275))
- **jobs:** Report a permanent failure to the queue with queue.ErrPermanent ([5bbb089](https://github.com/AlexGalhardo/vukem-tools/commit/5bbb089bfaca95f8b0e1a7baad7b039288f637e6))
- **jobs:** Settle a conversion on its last attempt instead of waiting for the reaper ([698356f](https://github.com/AlexGalhardo/vukem-tools/commit/698356f9c06fdc3e7dee53d960d895001204acf0))
- **pdf:** Settle a cancelled conversion instead of releasing it ([be4d80b](https://github.com/AlexGalhardo/vukem-tools/commit/be4d80b23f559ff6cc552090f835286bf8851575))
- **downloader:** Hold the platform allowlist at the tool boundary ([fee6eb6](https://github.com/AlexGalhardo/vukem-tools/commit/fee6eb6968a2281674906881dfc76268c6bcce1d))
- **editor:** Refuse an output name that is a path rather than a file name ([4f4e83c](https://github.com/AlexGalhardo/vukem-tools/commit/4f4e83c782fb51ce065d54b628955aa9604862b9))
- **infra:** Pin every engine's temp directory to its own job directory ([03e3336](https://github.com/AlexGalhardo/vukem-tools/commit/03e3336b9c855acbebaff522a1fb770da10db4c3))
- **downloader:** Ask yt-dlp for progress, which --quiet was suppressing ([34e04e1](https://github.com/AlexGalhardo/vukem-tools/commit/34e04e1f897975fbb0ad9215f36d0559cae89096))
- **ui:** Keep the sign-in pages registered and send missing pages home ([d53faeb](https://github.com/AlexGalhardo/vukem-tools/commit/d53faeba2fd4f52031ad49a34a8f37bb6fcbd0f9))

### Security

- **repo:** Add the security policy, contributing guide and code of conduct ([748d3bc](https://github.com/AlexGalhardo/vukem-tools/commit/748d3bc53f0688273dd600b4f296d17908ac6ba1))
- **repo:** Plan PDF parity with iLovePDF and cut the result lifetime to 5 minutes ([65e2bed](https://github.com/AlexGalhardo/vukem-tools/commit/65e2beda2fa227e049ddb4c54745ca1d98644e40))
- **telemetry:** Export conversion traces and metrics, with secrets scrubbed ([d259fd2](https://github.com/AlexGalhardo/vukem-tools/commit/d259fd252c3381352c25fb86ad080acc3c5fc9c5))
- **infra:** Assert the HTTP hardening instead of assuming it ([20fcc77](https://github.com/AlexGalhardo/vukem-tools/commit/20fcc773e0fc646c66bbac351d2106244306ffc9))
- **pdf:** Add the engine-backed PDF conversion library ([c304ae4](https://github.com/AlexGalhardo/vukem-tools/commit/c304ae4c695255f04dfafdd9a6063a2bf878b132))
- **pdf:** Register nineteen PDF tools behind one implementation ([c5a3b36](https://github.com/AlexGalhardo/vukem-tools/commit/c5a3b36472c811cceeac4269fa2d32f6d7ee9ee9))

### merge

- OpenAPI document and Scalar API reference ([8e3918b](https://github.com/AlexGalhardo/vukem-tools/commit/8e3918b5b33675e837ddd3e7969ba0c9110a208f))

[0.6.1]: https://github.com/AlexGalhardo/vukem-tools/compare/v0.6.0...v0.6.1
[0.6.0]: https://github.com/AlexGalhardo/vukem-tools/compare/v0.5.0...v0.6.0
[0.5.0]: https://github.com/AlexGalhardo/vukem-tools/compare/v0.4.0...v0.5.0
[0.4.0]: https://github.com/AlexGalhardo/vukem-tools/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/AlexGalhardo/vukem-tools/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/AlexGalhardo/vukem-tools/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/AlexGalhardo/vukem-tools/releases/tag/v0.1.0

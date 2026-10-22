package lottie

// Upgrading thorvg.wasm
//
// thorvg.wasm is the software-rasteriser build published as the npm package
// @thorvg/webcanvas. It is vendored rather than fetched so that a build is
// reproducible offline and the exact bytes are reviewable.
//
// To move to a new ThorVG release:
//
//	npm pack @thorvg/webcanvas
//	tar xzf thorvg-webcanvas-<version>.tgz
//	cp package/dist/thorvg.wasm internal/media/lottie/thorvg.wasm
//	cp package/LICENSE internal/media/lottie/LICENSE.thorvg
//
// Take dist/thorvg.wasm, not dist/thread/thorvg.wasm: the threaded build
// expects shared memory and worker imports this package does not provide.
//
// The published build minifies its export names — tvg_engine_init ships as
// "$b", malloc as "Zb" — and the mapping changes between releases. Recover the
// new table from the JavaScript glue beside the module, where it appears as a
// run of `_<c_name>=e.<minified>` assignments:
//
//	grep -o '_[A-Za-z_][A-Za-z_0-9]*=e\.[$A-Za-z_][$A-Za-z_0-9]*' package/dist/webcanvas.js
//
// Then update exportNames in symbols.go. A name that no longer resolves fails
// at load time with the symbol in the message rather than rendering nothing,
// and the package's tests render a known animation and check where its subject
// ends up, so a mismapped entry point does not pass silently.
//
// After upgrading, render a real-world corpus, not just this package's
// hand-written fixtures — those use a layer or two and never reach a host
// import, so they cannot tell you whether the new build leans on host
// behaviour this package does not provide. Google's Noto animated emoji are
// published as Lottie JSON and are a good stand-in for sticker complexity:
//
//	curl -s https://googlefonts.github.io/noto-emoji-animation/data/api.json
//	# then, per codepoint:
//	curl -s https://fonts.gstatic.com/s/e/notoemoji/latest/<codepoint>/lottie.json
//
// Render each one and check three things: none fail, none come out blank, and
// ReachedHostImports stays within the set TestRealWorldLottie allows. At the
// time of writing, 119 of them render with no failures and reach five stubbed
// imports.
//
// resizeHeapImport needs the same treatment. It is the single host import
// ThorVG reaches on the software render path — the hook malloc calls when
// linear memory runs out. It was identified by answering every import with a
// trap and running a full render to see which one fired; repeat that if the
// name stops matching. Registration fails loudly when the module no longer
// imports it.

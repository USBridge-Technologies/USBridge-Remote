package platform

// The usbridge:// link scheme is how an invitation e-mail (and the agent's
// QR/connect links) opens this app. Installers register it; a portable copy
// (AppImage, a zip) does not, so the app checks at start and offers to
// register itself -- IsURLSchemeRegistered / RegisterURLScheme, implemented
// per OS in urlscheme_*.go. OSes where the scheme is declared by the app
// package itself (macOS, Android, iOS) or that have none (browser) report
// registered.

// URLScheme is the scheme the app handles.
const URLScheme = "usbridge"

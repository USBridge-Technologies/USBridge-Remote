# PyroWave on Metal (macOS decode)

`pyrowave_metal.h`, `pyrowave_metal.mm` and `pyrowave_metal_shaders.inc` are copies of
rust-shine's `crates/pyrowave-sys/metal/` -- the native Metal port of the vendored PyroWave
encoder and decoder that the macOS host encodes with. Edit and regenerate them there
(`gen_shaders.sh` translates rust-shine's vendored shaders, the source of truth for the
bitstream), then copy the three files here unchanged.

The client uses the decoder only: `internal/service/pyrowave_decode_darwin.m`, built into
`build/libusbridge-pyrowave.a` by `scripts/build_pyrowave_macos.sh`.

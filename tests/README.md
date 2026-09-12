# End-to-End Tests

This directory is reserved for black-box and end-to-end tests that exercise GoVid
as an application rather than testing one package's internal behavior.

Unit tests belong next to the source files they cover (for example,
`download_engine_test.go` beside `download_engine.go`). Add tests here only when
they need to cross package or process boundaries, such as launching the built
application with controlled dependencies.

End-to-end tests must be self-contained and should use fixtures from `../testdata`.
They must not require real user preferences, network access, external download
accounts, or production media files.

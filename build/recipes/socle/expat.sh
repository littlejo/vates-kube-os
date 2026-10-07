# expat 2.8.4 -- XML parser, pulled in by fontconfig.
build() {
	autotools_build expat \
		--with-dev-urandom \
		--without-docbook \
		--without-examples \
		--without-tests \
		--without-xmlwf
}

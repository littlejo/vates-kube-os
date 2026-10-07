# pcre2 10.47 -- pulled in by glib. 8-bit only, no JIT: neither the 16/32-bit
# libraries nor the JIT are built.
build() {
	autotools_build pcre2 \
		--enable-pcre2-8 \
		--disable-pcre2-16 \
		--disable-pcre2-32 \
		--disable-jit
}

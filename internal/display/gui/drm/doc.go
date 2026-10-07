// Package drm: the screen, taken directly.
//
// A console is one full-screen window on a machine with nothing else to show. It
// does not need a compositor to decide where its window goes: it needs the
// framebuffer, one buffer to draw in, and a way to say "this part changed". That
// is what this package is, and it replaces cage -- along with the client-side
// title bar, the cursor trails and the OpenGL software stack that came with it.
//
// The mode is read from the connected screen, so the console adapts to whatever
// resolution the machine offers rather than assuming one.
package drm

// Package dashboard collects, for the console display, what one node knows
// about itself.
//
// It is separate from the drawing so that the same collection can be printed as
// text -- vates-dashboard --once -- which is what makes it testable over ssh, on a
// machine nobody is sitting in front of.
package dashboard

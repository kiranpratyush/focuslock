#pragma once

#include <QString>

class QWindow;

// Turns Qt windows into wlr-layer-shell surfaces, so the bar reserves a strip
// at the top of the screen and the block screen sits above every window
// without ever taking keyboard focus.
namespace Layer {
void makeBar(QWindow *window, int height);
void makeOverlay(QWindow *window);
// Shows the overlay at x,y (logical, relative to the monitor) on monitor, or hides it.
void place(QWindow *window, const QString &monitor, int x, int y, int w, int h, bool shown);
}

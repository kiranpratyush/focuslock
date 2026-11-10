#include "layer.h"

#include <LayerShellQt/window.h>
#include <QGuiApplication>
#include <QScreen>
#include <QWindow>

using LayerShellQt::Window;

void Layer::makeBar(QWindow *window, int height) {
    auto *layer = Window::get(window);
    layer->setScope("focuslock-bar");
    layer->setLayer(Window::LayerTop);
    layer->setAnchors(Window::Anchors(Window::AnchorTop | Window::AnchorLeft | Window::AnchorRight));
    layer->setExclusiveZone(height);
    layer->setKeyboardInteractivity(Window::KeyboardInteractivityNone);
    layer->setWantsToBeOnActiveScreen(true);
    window->setHeight(height);
    window->show();
}

void Layer::makeOverlay(QWindow *window) {
    auto *layer = Window::get(window);
    layer->setScope("focuslock-overlay");
    layer->setLayer(Window::LayerOverlay);
    layer->setAnchors(Window::Anchors(Window::AnchorTop | Window::AnchorLeft));
    layer->setExclusiveZone(-1);
    // The browser keeps the keyboard (Ctrl+Tab to a work tab) until you
    // click the block screen to type an explanation.
    layer->setKeyboardInteractivity(Window::KeyboardInteractivityOnDemand);
    layer->setActivateOnShow(false);
}

void Layer::place(QWindow *window, const QString &monitor, int x, int y, int w, int h, bool shown) {
    if (!shown || w <= 0 || h <= 0) {
        window->hide();
        return;
    }
    QScreen *screen = nullptr;
    for (QScreen *s : QGuiApplication::screens())
        if (s->name() == monitor)
            screen = s;
    // A layer surface is bound to its output when it's created, so moving to
    // another monitor means hiding and showing it again.
    auto *layer = Window::get(window);
    if (screen && window->screen() != screen) {
        window->hide();
        window->setScreen(screen);
        layer->setScreen(screen);
    }
    layer->setMargins(QMargins(x, y, 0, 0));
    layer->setDesiredSize(QSize(w, h));
    window->resize(w, h);
    window->show();
}

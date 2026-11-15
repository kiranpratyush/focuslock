#include "placer.h"

#include <LayerShellQt/window.h>
#include <QGuiApplication>
#include <QProcess>
#include <QScreen>
#include <QWindow>
#include <signal.h>

using LayerShellQt::Window;

Placer::~Placer() = default;

void Placer::setupBar(QWindow *window, int height) {
    auto *layer = Window::get(window);
    layer->setScope("focuslock-bar");
    layer->setLayer(Window::LayerTop);
    layer->setAnchors(Window::Anchors(Window::AnchorTop | Window::AnchorLeft | Window::AnchorRight));
    layer->setExclusiveZone(height);
    layer->setKeyboardInteractivity(Window::KeyboardInteractivityNone);
    layer->setWantsToBeOnActiveScreen(true);
    window->setHeight(height);
    window->show();
    m_bar = window;
}

void Placer::setupOverlay(QWindow *window) {
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

void Placer::place(QWindow *window, const QVariantMap &o) {
    const int x = o.value("x").toInt(), y = o.value("y").toInt(), w = o.value("w").toInt(), h = o.value("h").toInt();
    if (!window || o.isEmpty() || w <= 0 || h <= 0) {
        if (window)
            window->hide();
        return;
    }
    QScreen *screen = nullptr;
    for (QScreen *s : QGuiApplication::screens())
        if (s->name() == o.value("monitor").toString())
            screen = s;
    auto *layer = Window::get(window);
    // A layer surface is bound to its output when it's created, so moving
    // to another monitor means hiding and showing it again.
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

// Layer surfaces stay above windows by themselves.
void Placer::keepOnTop(QWindow *) {}

void Placer::activate(const QString &windowId) {
    if (!windowId.isEmpty())
        QProcess::startDetached("hyprctl", {"dispatch", "focuswindow", "address:" + windowId});
}

bool Placer::processAlive(qint64 pid) {
    return pid > 0 && ::kill(pid_t(pid), 0) == 0;
}

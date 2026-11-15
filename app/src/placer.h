#pragma once

#include <QObject>
#include <QVariantMap>

class QWindow;

// Puts the bar and the block screens where the engine says, one way per
// OS: wlr-layer-shell surfaces on Wayland (placer_wayland.cpp), an AppBar
// and topmost windows on Windows (placer_windows.cpp). QML calls it.
class Placer : public QObject {
    Q_OBJECT

public:
    using QObject::QObject;
    ~Placer() override;

    // The countdown strip across the top, reserved like the taskbar's.
    Q_INVOKABLE void setupBar(QWindow *window, int height);
    // A block screen: above every window, never taking the keyboard until
    // it's clicked.
    Q_INVOKABLE void setupOverlay(QWindow *window);
    // Shows window where says (a wire overlay: monitor/x/y/w/h, or
    // absolute/x/y/w/h), or hides it when where is empty.
    Q_INVOKABLE void place(QWindow *window, const QVariantMap &where);
    // Puts it back on top, in case another topmost window went over it.
    Q_INVOKABLE void keepOnTop(QWindow *window);
    // Gives focus to the covered window (the engine's windowId).
    Q_INVOKABLE void activate(const QString &windowId);

    static bool processAlive(qint64 pid);

private:
    QWindow *m_bar = nullptr;
};

#include "placer.h"

#include <QGuiApplication>
#include <QWindow>
#include <windows.h>
#include <shellapi.h>

// The bar is an AppBar: Windows moves other windows out of its strip, as
// it does for the taskbar. Block screens are topmost tool windows placed in
// physical pixels, which is how the engine measures the windows they cover,
// so mixed-DPI monitors line up without Qt's scaling in between.

static HWND hwndOf(QWindow *window) { return reinterpret_cast<HWND>(window->winId()); }

static void showWithoutFocus(QWindow *window) {
    // Read by Qt's Windows plugin: show without taking focus from what
    // you're working in.
    window->setProperty("_q_showWithoutActivating", true);
    if (!window->isVisible())
        window->show();
}

Placer::~Placer() {
    if (m_bar) {
        APPBARDATA abd{};
        abd.cbSize = sizeof(abd);
        abd.hWnd = hwndOf(m_bar);
        SHAppBarMessage(ABM_REMOVE, &abd);
    }
}

void Placer::setupBar(QWindow *window, int height) {
    window->setFlags(Qt::FramelessWindowHint | Qt::Tool | Qt::WindowStaysOnTopHint | Qt::WindowDoesNotAcceptFocus);
    showWithoutFocus(window);
    m_bar = window;

    const HWND hwnd = hwndOf(window);
    const int physical = MulDiv(height, int(GetDpiForWindow(hwnd)), 96);
    APPBARDATA abd{};
    abd.cbSize = sizeof(abd);
    abd.hWnd = hwnd;
    abd.uCallbackMessage = WM_APP + 1;
    SHAppBarMessage(ABM_NEW, &abd);
    abd.uEdge = ABE_TOP;
    abd.rc = RECT{0, 0, GetSystemMetrics(SM_CXSCREEN), physical};
    SHAppBarMessage(ABM_QUERYPOS, &abd);
    abd.rc.bottom = abd.rc.top + physical;
    SHAppBarMessage(ABM_SETPOS, &abd);
    SetWindowPos(hwnd, HWND_TOPMOST, abd.rc.left, abd.rc.top, abd.rc.right - abd.rc.left, physical,
                 SWP_NOACTIVATE | SWP_SHOWWINDOW);
    // Placer is destroyed with the app; take the strip back even on quit().
    QObject::connect(qApp, &QCoreApplication::aboutToQuit, this, [this] {
        if (!m_bar)
            return;
        APPBARDATA remove{};
        remove.cbSize = sizeof(remove);
        remove.hWnd = hwndOf(m_bar);
        SHAppBarMessage(ABM_REMOVE, &remove);
        m_bar = nullptr;
    });
}

void Placer::setupOverlay(QWindow *window) {
    window->setFlags(Qt::FramelessWindowHint | Qt::Tool | Qt::WindowStaysOnTopHint);
}

void Placer::place(QWindow *window, const QVariantMap &o) {
    const int x = o.value("x").toInt(), y = o.value("y").toInt(), w = o.value("w").toInt(), h = o.value("h").toInt();
    if (!window || o.isEmpty() || w <= 0 || h <= 0) {
        if (window)
            window->hide();
        return;
    }
    showWithoutFocus(window);
    SetWindowPos(hwndOf(window), HWND_TOPMOST, x, y, w, h, SWP_NOACTIVATE | SWP_SHOWWINDOW);
}

void Placer::keepOnTop(QWindow *window) {
    if (window && window->isVisible())
        SetWindowPos(hwndOf(window), HWND_TOPMOST, 0, 0, 0, 0, SWP_NOMOVE | SWP_NOSIZE | SWP_NOACTIVATE);
}

void Placer::activate(const QString &windowId) {
    bool ok = false;
    const auto handle = windowId.toULongLong(&ok, 16);
    if (ok && handle)
        SetForegroundWindow(reinterpret_cast<HWND>(handle)); // we have the click, so this is allowed
}

bool Placer::processAlive(qint64 pid) {
    HANDLE h = OpenProcess(SYNCHRONIZE, FALSE, DWORD(pid));
    if (!h)
        return false;
    const bool alive = WaitForSingleObject(h, 0) == WAIT_TIMEOUT;
    CloseHandle(h);
    return alive;
}

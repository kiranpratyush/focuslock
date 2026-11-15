QT += core gui network qml quick quickcontrols2

CONFIG += c++17 release
TARGET = focuslock
TEMPLATE = app

HEADERS += \
    src/backend.h \
    src/live.h \
    src/placer.h \
    src/theme.h \
    src/translator.h

SOURCES += \
    src/main.cpp \
    src/backend.cpp \
    src/live.cpp \
    src/theme.cpp \
    src/translator.cpp

# Where the bar and block screens go: layer-shell surfaces on Wayland, an
# AppBar and topmost windows on Windows.
unix:!macx {
    SOURCES += src/placer_wayland.cpp
    LIBS += -lLayerShellQtInterface
}
win32 {
    CONFIG -= debug_and_release debug_and_release_target
    SOURCES += src/placer_windows.cpp
    LIBS += -luser32 -lshell32
    RC_ICONS = pkgbuild/focuslock.ico
    DEFINES += NOMINMAX WIN32_LEAN_AND_MEAN
}

RESOURCES += src/resources.qrc

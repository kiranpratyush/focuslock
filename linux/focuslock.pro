QT += core gui network qml quick quickcontrols2

CONFIG += c++17 release
TARGET = focuslock
TEMPLATE = app

# Layer-shell surfaces for the countdown bar and the block screen.
LIBS += -lLayerShellQtInterface

HEADERS += \
    src/backend.h \
    src/live.h \
    src/layer.h \
    src/theme.h \
    src/translator.h

SOURCES += \
    src/main.cpp \
    src/backend.cpp \
    src/live.cpp \
    src/layer.cpp \
    src/theme.cpp \
    src/translator.cpp

RESOURCES += src/resources.qrc

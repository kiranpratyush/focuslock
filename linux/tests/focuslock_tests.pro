QT += core gui quick quickcontrols2 testlib
CONFIG += c++17 testcase
TARGET = focuslock_tests
TEMPLATE = app

INCLUDEPATH += ../src

HEADERS += ../src/backend.h ../src/live.h ../src/theme.h ../src/translator.h
SOURCES += focuslock_tests.cpp ../src/backend.cpp ../src/live.cpp ../src/theme.cpp ../src/translator.cpp

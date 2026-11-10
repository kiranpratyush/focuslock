// focuslock — a focus timer you can't casually quit, for Omarchy.
//
//   focuslock              the start window (or how long is left)
//   focuslock bar FILE     the countdown bar and block screen; the engine runs it
//   focuslock status|config|judge-check|judge-eval|start …   handed to focuslock-engine

#include <QDateTime>
#include <QGuiApplication>
#include <QIcon>
#include <QQmlApplicationEngine>
#include <QQmlContext>
#include <QQuickStyle>
#include <QQuickWindow>
#include <QTimer>
#include <QUrl>
#include <cstdio>
#include <cstring>
#include <unistd.h>

#include "backend.h"
#include "layer.h"
#include "live.h"
#include "theme.h"
#include "translator.h"

static const int kBarHeight = 26;

int main(int argc, char *argv[]) {
    // The command line belongs to the engine; exec it so exit codes and
    // output are exactly its own.
    if (argc > 1 && std::strcmp(argv[1], "bar") != 0 && std::strcmp(argv[1], "open") != 0) {
        const QByteArray engine = Backend::findEngine().toLocal8Bit();
        if (engine.isEmpty()) {
            std::fprintf(stderr, "focuslock: focuslock-engine not found\n");
            return 1;
        }
        argv[0] = const_cast<char *>(engine.constData());
        execv(engine.constData(), argv);
        std::perror("focuslock: exec focuslock-engine");
        return 1;
    }
    const bool bar = argc > 2 && std::strcmp(argv[1], "bar") == 0;

    QGuiApplication app(argc, argv);
    app.setApplicationName("focuslock");
    app.setApplicationVersion("0.1.0");
    app.setDesktopFileName("focuslock");
    app.setWindowIcon(QIcon::fromTheme("focuslock"));
    QQuickStyle::setStyle("Material");

    Translator translator;
    app.installTranslator(&translator);
    Theme theme;

    QQmlApplicationEngine engine;
    QObject::connect(&translator, &Translator::changed, &engine, &QQmlEngine::retranslate);
    engine.rootContext()->setContextProperty("translator", &translator);
    engine.rootContext()->setContextProperty("theme", &theme);

    if (bar) {
        Live live(QString::fromLocal8Bit(argv[2]));
        engine.rootContext()->setContextProperty("live", &live);
        engine.rootContext()->setContextProperty("barHeight", kBarHeight);
        engine.load(QUrl("qrc:/Bar.qml"));
        if (engine.rootObjects().isEmpty())
            return 1;
        QObject *root = engine.rootObjects().first();
        auto *barWindow = root->property("bar").value<QQuickWindow *>();
        auto *overlay = root->property("overlay").value<QQuickWindow *>();
        Layer::makeBar(barWindow, kBarHeight);
        Layer::makeOverlay(overlay);
        QObject::connect(&live, &Live::placementChanged, overlay,
                         [overlay](const QString &m, int x, int y, int w, int h, bool shown) {
                             Layer::place(overlay, m, x, y, w, h, shown);
                         });
        QObject::connect(&live, &Live::finished, &app, &QCoreApplication::quit);
        // The worker that started this bar owns it. If the worker dies, the
        // guard starts a new one, which starts its own bar, so this one goes.
        const pid_t parent = getppid();
        QTimer parentWatch;
        QObject::connect(&parentWatch, &QTimer::timeout, &app, [parent] {
            if (getppid() != parent)
                QCoreApplication::quit();
        });
        parentWatch.start(500);
        live.read(QDateTime::currentSecsSinceEpoch());
        return app.exec();
    }

    Backend backend;
    engine.rootContext()->setContextProperty("backend", &backend);
    engine.load(QUrl("qrc:/Main.qml"));
    if (engine.rootObjects().isEmpty())
        return 1;
    return app.exec();
}

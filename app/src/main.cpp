// focuslock — a focus timer you can't casually quit.
//
//   focuslock                    the start window (or how long is left)
//   focuslock bar ADDR PID       the countdown bar and block screens; the engine
//                                runs it, with its socket and its own pid
//   focuslock status|config|judge-check|judge-eval|start …   handed to focuslock-engine

#include <QGuiApplication>
#include <QIcon>
#include <QQmlApplicationEngine>
#include <QQmlContext>
#include <QQuickStyle>
#include <QTimer>
#include <QUrl>
#include <cstdio>
#include <cstring>

#ifdef Q_OS_WIN
#include <windows.h>
#else
#include <unistd.h>
#endif

#include "backend.h"
#include "live.h"
#include "placer.h"
#include "theme.h"
#include "translator.h"

static const int kBarHeight = 26;

// Runs focuslock-engine with this command line, so `focuslock status` and
// friends behave exactly like the engine. Returns its exit code.
static int forwardToEngine(char *argv[]) {
    const QString engine = Backend::findEngine();
    if (engine.isEmpty()) {
        std::fprintf(stderr, "focuslock: focuslock-engine not found\n");
        return 1;
    }
#ifdef Q_OS_WIN
    // This is a GUI program: borrow the console it was started from, then
    // hand the engine the same arguments, quoting untouched.
    AttachConsole(ATTACH_PARENT_PROCESS);
    std::wstring line = GetCommandLineW();
    size_t rest = 0;
    if (!line.empty() && line[0] == L'"')
        rest = line.find(L'"', 1) + 1;
    else
        rest = line.find(L' ');
    std::wstring command = L"\"" + engine.toStdWString() + L"\"" + (rest == std::wstring::npos ? L"" : line.substr(rest));
    STARTUPINFOW si{};
    si.cb = sizeof(si);
    PROCESS_INFORMATION pi{};
    if (!CreateProcessW(nullptr, command.data(), nullptr, nullptr, TRUE, 0, nullptr, nullptr, &si, &pi)) {
        std::fprintf(stderr, "focuslock: can't run focuslock-engine\n");
        return 1;
    }
    WaitForSingleObject(pi.hProcess, INFINITE);
    DWORD code = 1;
    GetExitCodeProcess(pi.hProcess, &code);
    CloseHandle(pi.hThread);
    CloseHandle(pi.hProcess);
    return int(code);
#else
    // Exec, so exit codes and output are exactly the engine's own.
    const QByteArray path = engine.toLocal8Bit();
    argv[0] = const_cast<char *>(path.constData());
    execv(path.constData(), argv);
    std::perror("focuslock: exec focuslock-engine");
    return 1;
#endif
}

int main(int argc, char *argv[]) {
    if (argc > 1 && std::strcmp(argv[1], "bar") != 0 && std::strcmp(argv[1], "open") != 0)
        return forwardToEngine(argv);
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
        Placer placer;
        Live live(QString::fromLocal8Bit(argv[2]));
        engine.rootContext()->setContextProperty("placer", &placer);
        engine.rootContext()->setContextProperty("live", &live);
        engine.rootContext()->setContextProperty("barHeight", kBarHeight);
        engine.load(QUrl("qrc:/Bar.qml"));
        if (engine.rootObjects().isEmpty())
            return 1;
        QObject::connect(&live, &Live::finished, &app, &QCoreApplication::quit);

        // The worker that started this bar owns it. If the worker dies, the
        // guard starts a new one, which starts its own bar, so this one goes.
        const qint64 worker = argc > 3 ? QByteArray(argv[3]).toLongLong() : 0;
        QTimer workerWatch;
        QObject::connect(&workerWatch, &QTimer::timeout, &app, [worker] {
            if (worker > 0 && !Placer::processAlive(worker))
                QCoreApplication::quit();
        });
        workerWatch.start(500);
        return app.exec();
    }

    Backend backend;
    engine.rootContext()->setContextProperty("backend", &backend);
    engine.load(QUrl("qrc:/Main.qml"));
    if (engine.rootObjects().isEmpty())
        return 1;
    return app.exec();
}

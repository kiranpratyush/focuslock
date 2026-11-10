#include "backend.h"

#include <QFileInfo>
#include <QJsonArray>
#include <QJsonDocument>
#include <QJsonObject>
#include <QRegularExpression>
#include <QStandardPaths>

#ifdef Q_OS_WIN
#include <windows.h>
#endif

// The engine is a console program; on Windows, don't flash a console
// window each time the app runs it.
static void hideConsole(QProcess &process) {
#ifdef Q_OS_WIN
    process.setCreateProcessArgumentsModifier([](QProcess::CreateProcessArguments *args) {
        args->flags |= CREATE_NO_WINDOW;
    });
#else
    Q_UNUSED(process);
#endif
}

Backend::Backend(const QString &engine, QObject *parent)
    : QObject(parent), m_engine(engine.isEmpty() ? findEngine() : engine) {
    m_process.setProcessChannelMode(QProcess::MergedChannels);
    hideConsole(m_process);
    connect(&m_process, &QProcess::readyRead, this, [this] {
        m_output += QString::fromUtf8(m_process.readAll());
        emit changed();
    });
    connect(&m_process, &QProcess::errorOccurred, this, [this](QProcess::ProcessError) {
        m_error = tr("Couldn't run focuslock-engine: %1").arg(m_process.errorString());
        emit changed();
    });
    connect(&m_process, &QProcess::finished, this, [this](int code, QProcess::ExitStatus status) {
        m_output += QString::fromUtf8(m_process.readAll());
        if (code != 0 || status != QProcess::NormalExit)
            m_error = m_output.trimmed().isEmpty() ? tr("The session didn't start.") : m_output.trimmed();
        refresh();
        if (m_active)
            emit started();
        emit changed();
    });

    // Pre-fill from the last session, as the Windows start window does.
    const auto last = QJsonDocument::fromJson(runEngine({"last-setup"})).object();
    auto join = [](const QJsonValue &v) {
        QStringList out;
        for (const auto &x : v.toArray()) out << x.toString();
        return out.join(", ");
    };
    if (!last.value("duration").toString().isEmpty())
        m_duration = last.value("duration").toString();
    m_task = last.value("task").toString();
    m_sites = join(last.value("allowDomains"));
    m_apps = join(last.value("allowApps"));
    refresh();
}

bool Backend::engineFound() const { return !m_engine.isEmpty() && QFileInfo(m_engine).isExecutable(); }

// The directory this program runs from. Works before QCoreApplication
// exists, which the command-line forwarding needs.
static QString programDirectory() {
#ifdef Q_OS_WIN
    wchar_t path[MAX_PATH];
    const DWORD n = GetModuleFileNameW(nullptr, path, MAX_PATH);
    return QFileInfo(QString::fromWCharArray(path, int(n))).absolutePath();
#else
    return QFileInfo(QStringLiteral("/proc/self/exe")).canonicalFilePath().section('/', 0, -2);
#endif
}

QString Backend::findEngine() {
    const QString env = qEnvironmentVariable("FOCUSLOCK_ENGINE");
    if (!env.isEmpty())
        return env;
#ifdef Q_OS_WIN
    const QStringList candidates{programDirectory() + "/focuslock-engine.exe"};
#else
    const QStringList candidates{programDirectory() + "/focuslock-engine", QStringLiteral("/usr/lib/focuslock/focuslock-engine")};
#endif
    for (const QString &path : candidates)
        if (QFileInfo(path).isExecutable())
            return path;
    return QStandardPaths::findExecutable("focuslock-engine");
}

int Backend::parseMinutes(const QString &input) {
    const QString text = input.trimmed().toLower();
    bool plain = false;
    const int n = text.toInt(&plain);
    if (plain)
        return n >= 1 ? n : -1;
    static const QRegularExpression re(R"(^(?:(\d+)h)?(?:(\d+)m)?$)");
    const auto m = re.match(text);
    if (!m.hasMatch() || text.isEmpty())
        return -1;
    const int minutes = m.captured(1).toInt() * 60 + m.captured(2).toInt();
    return minutes >= 1 ? minutes : -1;
}

QByteArray Backend::runEngine(const QStringList &args) {
    if (m_engine.isEmpty())
        return {};
    QProcess p;
    hideConsole(p);
    p.start(m_engine, args);
    if (!p.waitForFinished(3000))
        return {};
    return p.readAllStandardOutput();
}

void Backend::refresh() {
    const auto status = QJsonDocument::fromJson(runEngine({"status", "--json"})).object();
    m_active = status.value("active").toBool();
    m_endUnix = status.value("endUnix").toInteger();
    m_activeTask = status.value("task").toString();
    emit changed();
}

void Backend::start(const QString &duration, const QString &task, const QString &sites, const QString &apps) {
    if (busy())
        return;
    m_output.clear();
    m_error.clear();
    if (m_engine.isEmpty() || !QFileInfo(m_engine).isExecutable()) {
        m_error = tr("focuslock-engine isn't installed. Run bin/install from the focuslock repo.");
    } else if (parseMinutes(duration) < 0) {
        m_error = tr("\"%1\" isn't a duration. Use 50m, 1h30m, or 45.").arg(duration);
    } else {
        QStringList args{"start", duration.trimmed()};
        if (!task.trimmed().isEmpty())
            args << task.trimmed();
        args << "--no-prompt" << "--sites" << sites << "--apps" << apps;
        m_process.start(m_engine, args);
    }
    emit changed();
}

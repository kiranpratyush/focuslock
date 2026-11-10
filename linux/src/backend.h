#pragma once

#include <QObject>
#include <QProcess>

// The start window's side: talks to focuslock-engine, which owns sessions.
class Backend : public QObject {
    Q_OBJECT
    Q_PROPERTY(bool engineFound READ engineFound CONSTANT)
    Q_PROPERTY(bool active READ active NOTIFY changed)
    Q_PROPERTY(qint64 endUnix READ endUnix NOTIFY changed)
    Q_PROPERTY(QString activeTask READ activeTask NOTIFY changed)
    Q_PROPERTY(bool busy READ busy NOTIFY changed)
    Q_PROPERTY(QString output READ output NOTIFY changed)
    Q_PROPERTY(QString error READ error NOTIFY changed)
    Q_PROPERTY(QString duration READ duration CONSTANT)
    Q_PROPERTY(QString task READ task CONSTANT)
    Q_PROPERTY(QString sites READ sites CONSTANT)
    Q_PROPERTY(QString apps READ apps CONSTANT)

public:
    explicit Backend(const QString &engine = {}, QObject *parent = nullptr);

    static QString findEngine();
    // Minutes in a duration the engine accepts ("50m", "1h30m", "45"); -1 if invalid.
    static int parseMinutes(const QString &text);

    bool engineFound() const;
    bool active() const { return m_active; }
    qint64 endUnix() const { return m_endUnix; }
    QString activeTask() const { return m_activeTask; }
    bool busy() const { return m_process.state() != QProcess::NotRunning; }
    QString output() const { return m_output; }
    QString error() const { return m_error; }
    QString duration() const { return m_duration; }
    QString task() const { return m_task; }
    QString sites() const { return m_sites; }
    QString apps() const { return m_apps; }

    Q_INVOKABLE void refresh();
    Q_INVOKABLE void start(const QString &duration, const QString &task, const QString &sites, const QString &apps);

signals:
    void changed();
    void started();

private:
    QByteArray runEngine(const QStringList &args);

    QString m_engine;
    QProcess m_process;
    bool m_active = false;
    qint64 m_endUnix = 0;
    QString m_activeTask, m_output, m_error;
    QString m_duration = "50m", m_task, m_sites, m_apps;
};

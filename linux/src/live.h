#pragma once

#include <QJsonObject>
#include <QLocalSocket>
#include <QObject>
#include <QTimer>
#include <QVariantMap>

// The engine's frames, read from its socket (JSON lines), and the
// explanation box's commands sent back. It reconnects on its own: the
// worker is restarted if it's killed.
class Live : public QObject {
    Q_OBJECT
    Q_PROPERTY(QString remaining READ remaining NOTIFY changed)
    Q_PROPERTY(QString barText READ barText NOTIFY changed)
    Q_PROPERTY(bool overlayShown READ overlayShown NOTIFY changed)
    Q_PROPERTY(QVariantMap overlay READ overlay NOTIFY changed)
    Q_PROPERTY(int blockId READ blockId NOTIFY blockIdChanged)
    Q_PROPERTY(bool explainBusy READ explainBusy NOTIFY changed)
    Q_PROPERTY(QString explainStatus READ explainStatus NOTIFY changed)
    Q_PROPERTY(bool stale READ stale NOTIFY changed)

public:
    explicit Live(const QString &socketPath, QObject *parent = nullptr);
    ~Live() override;

    static QString formatRemaining(qint64 seconds);

    QString remaining() const { return formatRemaining(m_endUnix - m_now); }
    QString barText() const { return m_bar; }
    bool overlayShown() const { return !m_overlay.isEmpty(); }
    QVariantMap overlay() const { return m_overlay; }
    int blockId() const { return m_overlay.value("blockId").toInt(); }
    bool explainBusy() const { return m_explainBusy; }
    QString explainStatus() const { return m_explainStatus; }
    // No frame for a while: the worker is being restarted.
    bool stale() const { return m_now - m_lastFrame > 3; }

    // Applies one frame, as if it had just arrived at now (seconds).
    void applyFrame(const QJsonObject &frame, qint64 now);
    // Moves the clock on: the countdown, staleness, and the end.
    void tick(qint64 now);

    Q_INVOKABLE void explain(const QString &reason);
    Q_INVOKABLE void setTyping(bool on);

signals:
    void changed();
    void blockIdChanged();
    void placementChanged(const QString &monitor, int x, int y, int w, int h, bool shown);
    // An explanation was accepted: the block is gone.
    void released();
    void finished();

private:
    void send(const QJsonObject &command);
    void readFrames();

    QString m_path;
    QLocalSocket m_socket;
    QTimer m_clock, m_reconnect;
    QByteArray m_buffer;
    qint64 m_endUnix = 0, m_now = 0, m_lastFrame = 0;
    QString m_bar, m_placement, m_explainStatus;
    QVariantMap m_overlay;
    bool m_explainBusy = false, m_typing = false, m_finished = false;
};

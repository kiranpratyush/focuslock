#pragma once

#include <QObject>
#include <QTimer>
#include <QVariantMap>

// What the engine's worker wants drawn, read from live.json. The worker
// rewrites it twice a second, so polling is simpler than watching.
class Live : public QObject {
    Q_OBJECT
    Q_PROPERTY(QString remaining READ remaining NOTIFY changed)
    Q_PROPERTY(QString barText READ barText NOTIFY changed)
    Q_PROPERTY(bool overlayShown READ overlayShown NOTIFY changed)
    Q_PROPERTY(QVariantMap overlay READ overlay NOTIFY changed)
    Q_PROPERTY(bool stale READ stale NOTIFY changed)

public:
    explicit Live(const QString &path, QObject *parent = nullptr);

    static QString formatRemaining(qint64 seconds);

    QString remaining() const { return formatRemaining(m_endUnix - m_now); }
    QString barText() const { return m_bar; }
    bool overlayShown() const { return !m_overlay.isEmpty(); }
    QVariantMap overlay() const { return m_overlay; }
    // The worker hasn't written for a while: it's being restarted.
    bool stale() const { return m_now - m_lastWrite > 5; }
    qint64 endUnix() const { return m_endUnix; }

    void read(qint64 now);

signals:
    void changed();
    void placementChanged(const QString &monitor, int x, int y, int w, int h, bool shown);
    void finished();

private:
    QString m_path;
    QTimer m_timer;
    qint64 m_endUnix = 0, m_now = 0, m_lastWrite = 0;
    QString m_bar, m_placement;
    QVariantMap m_overlay;
};

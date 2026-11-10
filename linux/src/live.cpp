#include "live.h"

#include <QDateTime>
#include <QJsonDocument>

Live::Live(const QString &path, QObject *parent) : QObject(parent), m_path(path) {
    connect(&m_socket, &QLocalSocket::readyRead, this, &Live::readFrames);
    connect(&m_socket, &QLocalSocket::disconnected, this, [this] {
        m_buffer.clear();
        m_typing = false;
        m_reconnect.start();
    });
    connect(&m_socket, &QLocalSocket::errorOccurred, this, [this](QLocalSocket::LocalSocketError) {
        if (m_socket.state() != QLocalSocket::ConnectedState)
            m_reconnect.start();
    });
    m_reconnect.setSingleShot(true);
    m_reconnect.setInterval(500);
    connect(&m_reconnect, &QTimer::timeout, this, [this] {
        if (m_socket.state() == QLocalSocket::UnconnectedState)
            m_socket.connectToServer(m_path);
    });
    m_clock.setInterval(250);
    connect(&m_clock, &QTimer::timeout, this, [this] { tick(QDateTime::currentSecsSinceEpoch()); });

    // Started by the caller's event loop, once placementChanged is connected.
    if (!path.isEmpty()) {
        m_lastFrame = QDateTime::currentSecsSinceEpoch();
        m_clock.start();
        m_reconnect.setInterval(0);
        m_reconnect.start();
        m_reconnect.setInterval(500);
    }
}

// The socket is destroyed after the timers, and closing it emits
// disconnected, which would restart a timer that's already gone.
Live::~Live() {
    m_socket.disconnect(this);
    m_socket.abort();
}

QString Live::formatRemaining(qint64 s) {
    if (s < 0)
        s = 0;
    const qint64 h = s / 3600, m = s / 60 % 60, sec = s % 60;
    auto pad = [](qint64 v) { return QString::number(v).rightJustified(2, '0'); };
    return h > 0 ? QString("%1:%2:%3").arg(h).arg(pad(m), pad(sec)) : QString("%1:%2").arg(pad(m), pad(sec));
}

void Live::readFrames() {
    m_buffer += m_socket.readAll();
    const qint64 now = QDateTime::currentSecsSinceEpoch();
    int newline;
    while ((newline = m_buffer.indexOf('\n')) >= 0) {
        const QByteArray line = m_buffer.left(newline);
        m_buffer.remove(0, newline + 1);
        const auto doc = QJsonDocument::fromJson(line);
        if (doc.isObject() && doc.object().value("type").toString() == "frame")
            applyFrame(doc.object(), now);
    }
}

void Live::applyFrame(const QJsonObject &o, qint64 now) {
    m_lastFrame = now;
    m_endUnix = o.value("endUnix").toInteger();
    m_bar = o.value("bar").toString();
    const int oldBlock = blockId();
    m_overlay = o.value("overlay").toObject().toVariantMap();
    const auto explain = o.value("explain").toObject();
    m_explainBusy = explain.value("busy").toBool();
    m_explainStatus = explain.value("status").toString();
    if (explain.value("released").toBool())
        emit released();
    if (blockId() != oldBlock || m_overlay.isEmpty())
        setTyping(false);
    if (blockId() != oldBlock)
        emit blockIdChanged();

    const QString placement = m_overlay.isEmpty() ? QString()
        : QString("%1 %2 %3 %4 %5").arg(m_overlay.value("monitor").toString()).arg(m_overlay.value("x").toInt())
              .arg(m_overlay.value("y").toInt()).arg(m_overlay.value("w").toInt()).arg(m_overlay.value("h").toInt());
    if (placement != m_placement) {
        m_placement = placement;
        emit placementChanged(m_overlay.value("monitor").toString(), m_overlay.value("x").toInt(),
                              m_overlay.value("y").toInt(), m_overlay.value("w").toInt(),
                              m_overlay.value("h").toInt(), !m_overlay.isEmpty());
    }
    if (o.value("done").toBool())
        m_endUnix = qMin(m_endUnix, now);
    tick(now);
}

void Live::tick(qint64 now) {
    m_now = now;
    if (!m_finished && m_endUnix > 0 && now >= m_endUnix) {
        m_finished = true;
        m_overlay.clear();
        emit changed();
        emit placementChanged({}, 0, 0, 0, 0, false);
        emit finished();
        return;
    }
    emit changed();
}

void Live::explain(const QString &reason) {
    m_typing = false;
    send({{"type", "explain"}, {"reason", reason}});
}

void Live::setTyping(bool on) {
    if (on == m_typing)
        return;
    m_typing = on;
    send({{"type", "typing"}, {"on", on}});
}

void Live::send(const QJsonObject &command) {
    if (m_socket.state() != QLocalSocket::ConnectedState)
        return;
    m_socket.write(QJsonDocument(command).toJson(QJsonDocument::Compact) + '\n');
    m_socket.flush();
}

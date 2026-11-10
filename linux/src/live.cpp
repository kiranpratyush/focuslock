#include "live.h"

#include <QDateTime>
#include <QFile>
#include <QFileInfo>
#include <QJsonDocument>
#include <QJsonObject>

Live::Live(const QString &path, QObject *parent) : QObject(parent), m_path(path) {
    m_timer.setInterval(250);
    connect(&m_timer, &QTimer::timeout, this, [this] { read(QDateTime::currentSecsSinceEpoch()); });
    // The first read is the caller's, once it has connected placementChanged.
    if (!path.isEmpty())
        m_timer.start();
}

QString Live::formatRemaining(qint64 s) {
    if (s < 0)
        s = 0;
    const qint64 h = s / 3600, m = s / 60 % 60, sec = s % 60;
    auto pad = [](qint64 v) { return QString::number(v).rightJustified(2, '0'); };
    return h > 0 ? QString("%1:%2:%3").arg(h).arg(pad(m), pad(sec)) : QString("%1:%2").arg(pad(m), pad(sec));
}

void Live::read(qint64 now) {
    m_now = now;
    QFile file(m_path);
    if (file.open(QIODevice::ReadOnly)) {
        m_lastWrite = QFileInfo(file).lastModified().toSecsSinceEpoch();
        const auto o = QJsonDocument::fromJson(file.readAll()).object();
        if (!o.isEmpty()) {
            m_endUnix = o.value("endUnix").toInteger();
            m_bar = o.value("bar").toString();
            m_overlay = o.value("overlay").toObject().toVariantMap();
        }
    }
    if (m_endUnix > 0 && now >= m_endUnix) {
        m_overlay.clear();
        emit changed();
        emit placementChanged({}, 0, 0, 0, 0, false);
        emit finished();
        return;
    }
    const QString placement = m_overlay.isEmpty() ? QString()
        : QString("%1 %2 %3 %4 %5").arg(m_overlay.value("monitor").toString()).arg(m_overlay.value("x").toInt())
              .arg(m_overlay.value("y").toInt()).arg(m_overlay.value("w").toInt()).arg(m_overlay.value("h").toInt());
    if (placement != m_placement) {
        m_placement = placement;
        emit placementChanged(m_overlay.value("monitor").toString(), m_overlay.value("x").toInt(),
                              m_overlay.value("y").toInt(), m_overlay.value("w").toInt(),
                              m_overlay.value("h").toInt(), !m_overlay.isEmpty());
    }
    emit changed();
}

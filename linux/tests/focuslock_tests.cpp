#include <QFile>
#include <QJsonDocument>
#include <QJsonObject>
#include <QLocalServer>
#include <QLocalSocket>
#include <QSignalSpy>
#include <QTemporaryDir>
#include <QtTest>

#include "backend.h"
#include "live.h"
#include "theme.h"
#include "translator.h"

class FocuslockTests : public QObject {
    Q_OBJECT

private slots:
    void durationsMatchTheEngine()
    {
        QCOMPARE(Backend::parseMinutes("45"), 45);
        QCOMPARE(Backend::parseMinutes("50m"), 50);
        QCOMPARE(Backend::parseMinutes(" 1h30m "), 90);
        QCOMPARE(Backend::parseMinutes("2h"), 120);
        QCOMPARE(Backend::parseMinutes("0"), -1);
        QCOMPARE(Backend::parseMinutes("soon"), -1);
        QCOMPARE(Backend::parseMinutes(""), -1);
    }

    void missingEngineIsReported()
    {
        QTemporaryDir dir;
        Backend backend(dir.filePath("no-such-engine"));
        backend.start("50m", "task", "", "");
        QVERIFY(!backend.error().isEmpty());
        QVERIFY(!backend.active());
    }

    void remainingIsFormatted()
    {
        QCOMPARE(Live::formatRemaining(65), QString("01:05"));
        QCOMPARE(Live::formatRemaining(3725), QString("1:02:05"));
        QCOMPARE(Live::formatRemaining(-3), QString("00:00"));
    }

    void liveAppliesFramesAndPlacement()
    {
        Live live({});
        QSignalSpy placed(&live, &Live::placementChanged);
        QSignalSpy block(&live, &Live::blockIdChanged);
        live.applyFrame(json(R"({"type":"frame","endUnix":2000,"bar":"ends 15:04",
            "overlay":{"title":"x","explain":true,"blockId":3,"monitor":"DP-1","x":10,"y":20,"w":300,"h":200},
            "explain":{"busy":true,"status":"Checking your reason…"}})"), 1000);
        QCOMPARE(live.barText(), QString("ends 15:04"));
        QCOMPARE(live.remaining(), QString("16:40"));
        QVERIFY(live.overlayShown());
        QCOMPARE(live.blockId(), 3);
        QCOMPARE(block.count(), 1);
        QVERIFY(live.explainBusy());
        QCOMPARE(live.explainStatus(), QString("Checking your reason…"));
        QCOMPARE(placed.count(), 1);
        QCOMPARE(placed.first().at(0).toString(), QString("DP-1"));
        QCOMPARE(placed.first().at(3).toInt(), 300);
        live.applyFrame(json(R"({"type":"frame","endUnix":2000,"overlay":{"blockId":3,"monitor":"DP-1","x":10,"y":20,"w":300,"h":200}})"), 1001);
        QCOMPARE(placed.count(), 1); // unchanged placement isn't re-sent
        QCOMPARE(block.count(), 1);
        QVERIFY(!live.stale());
        live.tick(1010);
        QVERIFY(live.stale());
    }

    void liveFinishesWhenTheTimerEnds()
    {
        Live live({});
        QSignalSpy finished(&live, &Live::finished);
        live.applyFrame(json(R"({"type":"frame","endUnix":2000})"), 1999);
        QCOMPARE(finished.count(), 0);
        live.tick(2000);
        QCOMPARE(finished.count(), 1);
        live.tick(2001);
        QCOMPARE(finished.count(), 1);
    }

    void liveFinishesOnADoneFrame()
    {
        Live live({});
        QSignalSpy finished(&live, &Live::finished);
        live.applyFrame(json(R"({"type":"frame","endUnix":5000,"done":true})"), 1000);
        QCOMPARE(finished.count(), 1);
    }

    void liveTalksToTheEngineSocket()
    {
        // A fake engine: sends a frame, then reads what the box sends back.
        QTemporaryDir dir;
        const QString path = dir.filePath("engine.sock");
        QLocalServer server;
        QVERIFY(server.listen(path));
        Live live(path);
        QTRY_VERIFY(server.hasPendingConnections());
        QLocalSocket *engine = server.nextPendingConnection();
        QSignalSpy released(&live, &Live::released);
        engine->write(R"({"type":"frame","endUnix":9999999999,"bar":"hi","overlay":{"blockId":1}})" "\n"
                      R"({"type":"frame","endUnix":9999999999,"bar":"again","explain":{"released":true}})" "\n");
        engine->flush();
        QTRY_COMPARE(live.barText(), QString("again"));
        QCOMPARE(released.count(), 1);

        live.applyFrame(json(R"({"type":"frame","endUnix":9999999999,"overlay":{"blockId":2}})"), 1000);
        live.setTyping(true);
        live.explain("the API docs are on this page");
        QStringList lines;
        QTRY_VERIFY((lines += QString::fromUtf8(engine->readAll()).split('\n', Qt::SkipEmptyParts), lines.size() >= 2));
        QCOMPARE(QJsonDocument::fromJson(lines[0].toUtf8()).object().value("type").toString(), QString("typing"));
        const auto explain = QJsonDocument::fromJson(lines[1].toUtf8()).object();
        QCOMPARE(explain.value("type").toString(), QString("explain"));
        QCOMPARE(explain.value("reason").toString(), QString("the API docs are on this page"));
    }

    void themeFallsBack()
    {
        QTemporaryDir dir;
        Theme theme(dir.path());
        QCOMPARE(theme.accent(), QString("#FFD60A"));
        QCOMPARE(theme.accentForeground(), QString("black"));
    }

    void themeReadsAccent()
    {
        QTemporaryDir dir;
        QDir(dir.path()).mkpath("theme");
        write(dir.path() + "/theme/colors.toml", "accent = \"#112233\"\n");
        Theme theme(dir.path());
        QCOMPARE(theme.accent(), QString("#112233"));
    }

    void translatorFallsBackToEnglish()
    {
        QTemporaryDir dir;
        Translator translator(dir.path() + "/language", dir.path());
        QCOMPARE(translator.language(), QString("en"));
        QVERIFY(translator.translate("Main", "Start focus").isNull());
    }

private:
    static QJsonObject json(const char *text) { return QJsonDocument::fromJson(text).object(); }

    static void write(const QString &path, const QByteArray &contents)
    {
        QFile file(path);
        QVERIFY(file.open(QIODevice::WriteOnly));
        file.write(contents);
    }
};

QTEST_MAIN(FocuslockTests)
#include "focuslock_tests.moc"

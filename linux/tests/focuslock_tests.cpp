#include <QFile>
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

    void liveReadsOverlayAndPlacement()
    {
        QTemporaryDir dir;
        const QString path = dir.filePath("live.json");
        write(path, R"({"endUnix":2000,"bar":"ends 15:04","overlay":{"Title":"x","monitor":"DP-1","x":10,"y":20,"w":300,"h":200}})");
        Live live(path);
        QSignalSpy placed(&live, &Live::placementChanged);
        live.read(1000);
        QCOMPARE(live.barText(), QString("ends 15:04"));
        QVERIFY(live.overlayShown());
        QCOMPARE(placed.count(), 1);
        QCOMPARE(placed.first().at(0).toString(), QString("DP-1"));
        QCOMPARE(placed.first().at(3).toInt(), 300);
        live.read(1001);
        QCOMPARE(placed.count(), 1); // unchanged placement isn't re-sent
    }

    void liveFinishesWhenTheTimerEnds()
    {
        QTemporaryDir dir;
        const QString path = dir.filePath("live.json");
        write(path, R"({"endUnix":2000,"bar":""})");
        Live live(path);
        QSignalSpy finished(&live, &Live::finished);
        live.read(2000);
        QCOMPARE(finished.count(), 1);
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
    static void write(const QString &path, const QByteArray &contents)
    {
        QFile file(path);
        QVERIFY(file.open(QIODevice::WriteOnly));
        file.write(contents);
    }
};

QTEST_MAIN(FocuslockTests)
#include "focuslock_tests.moc"

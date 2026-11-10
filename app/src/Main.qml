import QtQuick
import QtQuick.Controls
import QtQuick.Controls.Material
import QtQuick.Layouts

// The start window: how long, what you're working on, and what this session
// also needs. Once a session runs it only says how long is left — there is
// no stop button, by design.
ApplicationWindow {
    id: win
    width: 560
    height: 600
    minimumWidth: 460
    minimumHeight: 520
    visible: true
    title: "FocusLock"
    color: "#0e0e10"

    Material.theme: Material.Dark
    Material.accent: theme.accent
    Material.roundedScale: theme.radius

    property bool helpVisible: false
    property int now: Math.floor(Date.now() / 1000)

    function startSession() {
        if (!backend.active && !backend.busy)
            backend.start(duration.text, task.text, sites.text, apps.text)
    }
    function remaining() {
        const s = Math.max(0, backend.endUnix - now)
        const h = Math.floor(s / 3600), m = Math.floor(s / 60) % 60, sec = s % 60
        const pad = v => (v < 10 ? "0" : "") + v
        return (h > 0 ? h + ":" : "") + pad(m) + ":" + pad(sec)
    }

    Timer { interval: 1000; running: true; repeat: true; onTriggered: { win.now = Math.floor(Date.now() / 1000); if (backend.active && backend.endUnix <= win.now) backend.refresh() } }
    Connections { target: backend; function onStarted() { closeTimer.start() } }
    Timer { id: closeTimer; interval: 2500; onTriggered: Qt.quit() }

    Shortcut { sequence: "?"; context: Qt.ApplicationShortcut; enabled: !win.activeFocusItem || !(win.activeFocusItem instanceof TextInput) || win.helpVisible; onActivated: win.helpVisible = !win.helpVisible }
    Shortcut { sequence: "F1"; context: Qt.ApplicationShortcut; onActivated: win.helpVisible = !win.helpVisible }
    Shortcut { sequences: ["Escape", "Ctrl+Q"]; context: Qt.ApplicationShortcut; onActivated: win.helpVisible ? win.helpVisible = false : Qt.quit() }
    Shortcut { sequences: ["Ctrl+Return", "Ctrl+Enter"]; context: Qt.ApplicationShortcut; onActivated: win.startSession() }

    Item {
        anchors.fill: parent
        LayoutMirroring.enabled: Qt.application.layoutDirection === Qt.RightToLeft
        LayoutMirroring.childrenInherit: true

        ColumnLayout {
            anchors.fill: parent
            anchors.margins: 28
            spacing: 14
            visible: !backend.active

            Label { text: qsTr("Plan a focus session"); font.pixelSize: 24; font.bold: true }
            Label {
                Layout.fillWidth: true
                wrapMode: Text.WordWrap
                opacity: 0.7
                text: qsTr("Until the timer ends, entertainment and anything off your task gets covered. There's no stop button.")
            }
            TextField {
                id: duration
                Layout.fillWidth: true
                placeholderText: qsTr("How long? (50m, 1h30m, 45)")
                text: backend.duration
                focus: true
                Keys.onReturnPressed: win.startSession()
                Keys.onEnterPressed: win.startSession()
            }
            TextField {
                id: task
                Layout.fillWidth: true
                placeholderText: qsTr("What are you working on?")
                text: backend.task
            }
            TextField {
                id: sites
                Layout.fillWidth: true
                placeholderText: qsTr("Sites this session needs (comma-separated)")
                text: backend.sites
            }
            TextField {
                id: apps
                Layout.fillWidth: true
                placeholderText: qsTr("Apps this session needs (comma-separated)")
                text: backend.apps
                Keys.onReturnPressed: win.startSession()
                Keys.onEnterPressed: win.startSession()
            }
            Label {
                Layout.fillWidth: true
                visible: !backend.engineFound || backend.error !== ""
                wrapMode: Text.WordWrap
                color: "#f87171"
                text: backend.engineFound ? backend.error : qsTr("focuslock-engine isn't installed. Run bin/install from the focuslock repo.")
            }
            Label {
                Layout.fillWidth: true
                Layout.fillHeight: true
                visible: backend.output !== "" && backend.error === ""
                wrapMode: Text.WordWrap
                font.family: "monospace"
                opacity: 0.8
                text: backend.output
            }
            Item { Layout.fillHeight: true; visible: backend.output === "" || backend.error !== "" }
            RowLayout {
                Layout.fillWidth: true
                Label { text: qsTr("Ctrl+Enter starts · Esc cancels · F1 shortcuts"); opacity: 0.5; Layout.fillWidth: true; elide: Text.ElideRight }
                Button {
                    text: backend.busy ? qsTr("Starting…") : qsTr("Start focus")
                    highlighted: true
                    enabled: backend.engineFound && !backend.busy
                    onClicked: win.startSession()
                }
            }
        }

        ColumnLayout {
            anchors.centerIn: parent
            width: parent.width - 56
            spacing: 10
            visible: backend.active

            Label { Layout.alignment: Qt.AlignHCenter; text: qsTr("Focus session running"); opacity: 0.7 }
            Label { Layout.alignment: Qt.AlignHCenter; text: win.remaining(); font.pixelSize: 64; font.bold: true; color: theme.accent }
            Label {
                Layout.fillWidth: true
                horizontalAlignment: Text.AlignHCenter
                wrapMode: Text.WordWrap
                visible: backend.activeTask !== ""
                text: backend.activeTask
            }
            Label {
                Layout.alignment: Qt.AlignHCenter
                opacity: 0.5
                text: qsTr("Ends at %1. Esc closes this window; the session keeps going.")
                      .arg(new Date(backend.endUnix * 1000).toLocaleTimeString(Qt.locale(), Locale.ShortFormat) + (translator.language, ""))
            }
        }

        Rectangle {
            anchors.fill: parent
            visible: win.helpVisible
            color: "#e60e0e10"
            ColumnLayout {
                anchors.centerIn: parent
                spacing: 8
                Label { text: qsTr("Shortcuts"); font.bold: true; font.pixelSize: 18 }
                Repeater {
                    model: [
                        ["Ctrl+Enter", qsTr("Start the session")],
                        ["Enter", qsTr("Start, from the duration or apps box")],
                        ["Tab", qsTr("Next box")],
                        ["Esc", qsTr("Close the window")],
                        ["F1 / ?", qsTr("Show or hide shortcuts")]
                    ]
                    RowLayout {
                        spacing: 16
                        Label { text: modelData[0]; color: theme.accent; Layout.preferredWidth: 110; font.family: "monospace" }
                        Label { text: modelData[1] }
                    }
                }
            }
        }
    }
}

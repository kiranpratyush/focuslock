import QtQuick
import QtQuick.Controls
import QtQuick.Controls.Material
import QtQuick.Layouts

// What a block screen says about the window under it. The explanation box
// appears only where the engine offers one (the main block screen).
Item {
    id: card
    property var v: ({})
    readonly property color accent: v.accent === "entertainment" ? "#f43f5e" : v.accent === "music" ? "#38bdf8" : "#facc15"
    signal submit(string reason)
    signal back()

    function clear() { reason.text = ""; reason.focus = false }

    Material.theme: Material.Dark
    Material.accent: card.accent

    Rectangle {
        anchors.centerIn: parent
        width: Math.min(parent.width - 48, 720)
        height: Math.min(parent.height - 48, body.implicitHeight + 64)
        radius: theme.radius
        color: "#1a1a20"
        clip: true
        Rectangle { width: 4; height: parent.height; color: card.accent }

        ColumnLayout {
            id: body
            anchors.fill: parent
            anchors.margins: 32
            spacing: 12
            LayoutMirroring.enabled: Qt.application.layoutDirection === Qt.RightToLeft
            LayoutMirroring.childrenInherit: true

            Label { text: card.v.kicker || ""; color: card.accent; font.bold: true; font.letterSpacing: 1.2; font.pixelSize: 13 }
            Label { Layout.fillWidth: true; text: card.v.title || ""; font.pixelSize: 28; font.bold: true; wrapMode: Text.WordWrap; maximumLineCount: 3; elide: Text.ElideRight; color: "white" }
            Label { text: card.v.where || ""; opacity: 0.6; color: "white" }
            Label { Layout.fillWidth: true; text: card.v.why || ""; wrapMode: Text.WordWrap; font.pixelSize: 16; color: "white" }
            Label { Layout.fillWidth: true; text: card.v.task || ""; wrapMode: Text.WordWrap; font.pixelSize: 16; color: "white"; opacity: 0.85 }
            Item { Layout.fillHeight: true; Layout.minimumHeight: 8 }

            // "Need this for your task?" Click to type: the covered window
            // keeps the keyboard until then. Enter sends, Esc gives it back.
            Label { Layout.fillWidth: true; visible: card.v.explain === true; text: card.v.prompt || ""; wrapMode: Text.WordWrap; color: "white"; opacity: 0.85 }
            RowLayout {
                Layout.fillWidth: true
                visible: card.v.explain === true
                spacing: 10
                TextField {
                    id: reason
                    Layout.fillWidth: true
                    Material.roundedScale: theme.radius
                    placeholderText: qsTr("How does this help your task?")
                    maximumLength: 300
                    enabled: !live.explainBusy
                    onActiveFocusChanged: live.setTyping(activeFocus)
                    Keys.onReturnPressed: send()
                    Keys.onEnterPressed: send()
                    Keys.onEscapePressed: { reason.focus = false; live.setTyping(false); card.back() }
                    function send() {
                        if (!live.explainBusy && text.trim().length > 0)
                            card.submit(text.trim())
                    }
                }
                Button {
                    text: live.explainBusy ? qsTr("Checking…") : qsTr("Explain")
                    Material.roundedScale: theme.radius
                    highlighted: true
                    enabled: !live.explainBusy && reason.text.trim().length > 0
                    onClicked: reason.send()
                }
            }
            Label { Layout.fillWidth: true; visible: card.v.explain === true && live.explainStatus !== ""; text: live.explainStatus; wrapMode: Text.WordWrap; color: card.accent }

            Label { Layout.fillWidth: true; text: card.v.footer || ""; wrapMode: Text.WordWrap; font.bold: true; color: "white" }
            Label { Layout.fillWidth: true; text: card.v.stats || ""; wrapMode: Text.WordWrap; opacity: 0.5; color: "white" }
        }
    }
}

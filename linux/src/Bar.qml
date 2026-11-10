import QtQuick
import QtQuick.Controls
import QtQuick.Controls.Material
import QtQuick.Layouts

// Two layer-shell surfaces, set up in main.cpp: the countdown bar across the
// top, and the block screen the engine places over a blocked window.
QtObject {
    function accentFor(cat) {
        return cat === "entertainment" ? "#f43f5e" : cat === "music" ? "#38bdf8" : "#facc15"
    }

    property Window bar: Window {
        visible: false
        color: "#18181b"
        Material.theme: Material.Dark
        Material.accent: theme.accent

        Rectangle { anchors.left: parent.left; anchors.right: parent.right; anchors.bottom: parent.bottom; height: 2; color: theme.accent }
        RowLayout {
            anchors.fill: parent
            anchors.leftMargin: 14
            anchors.rightMargin: 14
            spacing: 12
            LayoutMirroring.enabled: Qt.application.layoutDirection === Qt.RightToLeft
            LayoutMirroring.childrenInherit: true
            Label { text: qsTr("FOCUS"); font.bold: true; font.letterSpacing: 1.5; color: theme.accent }
            Label { text: qsTr("%1 left").arg(live.remaining); font.bold: true; font.family: "monospace" }
            Label {
                Layout.fillWidth: true
                elide: Text.ElideRight
                opacity: 0.75
                text: live.stale ? qsTr("Reconnecting to the engine…") : live.barText
            }
        }
    }

    property Window overlay: Window {
        id: ov
        visible: false
        color: "#f50c0c10"
        readonly property var v: live.overlay
        readonly property color accent: accentFor(v.accent || "")

        Rectangle {
            anchors.centerIn: parent
            width: Math.min(parent.width - 48, 720)
            height: Math.min(parent.height - 48, card.implicitHeight + 64)
            radius: theme.radius
            color: "#1a1a20"
            clip: true
            Rectangle { width: 4; height: parent.height; color: ov.accent }

            ColumnLayout {
                id: card
                anchors.fill: parent
                anchors.margins: 32
                spacing: 12
                LayoutMirroring.enabled: Qt.application.layoutDirection === Qt.RightToLeft
                LayoutMirroring.childrenInherit: true
                Label { text: ov.v.kicker || ""; color: ov.accent; font.bold: true; font.letterSpacing: 1.2; font.pixelSize: 13 }
                Label { Layout.fillWidth: true; text: ov.v.title || ""; font.pixelSize: 28; font.bold: true; wrapMode: Text.WordWrap; maximumLineCount: 3; elide: Text.ElideRight; color: "white" }
                Label { text: ov.v.where || ""; opacity: 0.6; color: "white" }
                Label { Layout.fillWidth: true; text: ov.v.why || ""; wrapMode: Text.WordWrap; font.pixelSize: 16; color: "white" }
                Label { Layout.fillWidth: true; text: ov.v.task || ""; wrapMode: Text.WordWrap; font.pixelSize: 16; color: "white"; opacity: 0.85 }
                Item { Layout.fillHeight: true; Layout.minimumHeight: 8 }
                Label { Layout.fillWidth: true; text: ov.v.footer || ""; wrapMode: Text.WordWrap; font.bold: true; color: "white" }
                Label { Layout.fillWidth: true; text: ov.v.stats || ""; wrapMode: Text.WordWrap; opacity: 0.5; color: "white" }
            }
        }
    }
}

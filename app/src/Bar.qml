import QtQuick
import QtQuick.Controls
import QtQuick.Controls.Material
import QtQuick.Layouts

// The countdown bar across the top, the block screen over the window in
// front, and a few more for blocked windows on other monitors. Placer puts
// them where the engine says, the native way on each OS.
QtObject {
    id: root

    // Where a block screen goes, as a string: re-placed only when it moves.
    function placeKey(o) {
        return o ? [o.monitor, o.absolute, o.x, o.y, o.w, o.h].join(" ") : ""
    }

    property Window bar: Window {
        visible: false
        color: "#18181b"
        Material.theme: Material.Dark
        Material.accent: theme.accent
        Component.onCompleted: placer.setupBar(this, barHeight)

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
        readonly property string where: root.placeKey(live.overlayShown ? v : null)
        Component.onCompleted: placer.setupOverlay(this)
        onWhereChanged: placer.place(this, live.overlayShown ? v : ({}))

        BlockCard {
            id: card
            anchors.fill: parent
            v: ov.v
            onSubmit: reason => live.explain(reason)
            onBack: placer.activate(ov.v.windowId || "")
        }

        // A new block, or one lifted by an accepted explanation, starts with
        // an empty box; an accepted one hands the keyboard back.
        property string lastWindow: ""
        onVChanged: if (v && v.windowId) lastWindow = v.windowId
        Connections {
            target: live
            function onBlockIdChanged() { card.clear() }
            function onReleased() { card.clear(); placer.activate(ov.lastWindow) }
        }
    }

    // Blocked windows on other monitors: covered, never closed. Click one to
    // bring its window to the front, where the main block screen takes over.
    property Instantiator sides: Instantiator {
        model: 4
        delegate: Window {
            id: side
            required property int index
            visible: false
            color: "#f50c0c10"
            readonly property var v: index < live.sides.length ? live.sides[index] : null
            readonly property string where: root.placeKey(v)
            Component.onCompleted: placer.setupOverlay(this)
            onWhereChanged: placer.place(this, v || ({}))
            BlockCard { anchors.fill: parent; v: side.v || ({}) }
            TapHandler { onTapped: placer.activate(side.v ? side.v.windowId : "") }
        }
    }

    // Other always-on-top windows can go over ours; put them back.
    property Timer onTop: Timer {
        interval: 1000
        running: true
        repeat: true
        onTriggered: {
            placer.keepOnTop(root.bar)
            placer.keepOnTop(root.overlay)
            for (let i = 0; i < root.sides.count; i++)
                placer.keepOnTop(root.sides.objectAt(i))
        }
    }
}

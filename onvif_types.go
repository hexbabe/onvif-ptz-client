package onvifptzclient

import (
	"encoding/xml"
)

// --- Constants for Space URIs ---
const (
	AbsolutePanTiltPositionGenericSpace     = "http://www.onvif.org/ver10/tptz/PanTiltSpaces/PositionGenericSpace"
	AbsolutePanTiltPositionSphericalDegrees = "http://www.onvif.org/ver10/tptz/PanTiltSpaces/SphericalPositionSpaceDegrees"
	AbsoluteZoomPositionGenericSpace        = "http://www.onvif.org/ver10/tptz/ZoomSpaces/PositionGenericSpace"

	RelativePanTiltTranslationGenericSpace     = "http://www.onvif.org/ver10/tptz/PanTiltSpaces/TranslationGenericSpace"
	RelativePanTiltTranslationSphericalDegrees = "http://www.onvif.org/ver10/tptz/PanTiltSpaces/SphericalTranslationSpaceDegrees"
	RelativeZoomTranslationGenericSpace        = "http://www.onvif.org/ver10/tptz/ZoomSpaces/TranslationGenericSpace"

	ContinuousPanTiltVelocityGenericSpace = "http://www.onvif.org/ver10/tptz/PanTiltSpaces/VelocityGenericSpace"
	ContinuousZoomVelocityGenericSpace    = "http://www.onvif.org/ver10/tptz/ZoomSpaces/VelocityGenericSpace"
)

// --- Custom Structs for GetStatus Response ---

type CustomGetStatusEnvelope struct {
	XMLName xml.Name            `xml:"http://www.w3.org/2003/05/soap-envelope Envelope"`
	Body    CustomGetStatusBody `xml:"http://www.w3.org/2003/05/soap-envelope Body"`
}

type CustomGetStatusBody struct {
	GetResponse CustomGetStatusResponse `xml:"http://www.onvif.org/ver20/ptz/wsdl GetStatusResponse"`
}

type CustomGetStatusResponse struct {
	PTZStatus CustomPTZStatus `xml:"http://www.onvif.org/ver20/ptz/wsdl PTZStatus"`
}

type CustomPTZStatus struct {
	Position   CustomPosition   `xml:"http://www.onvif.org/ver10/schema Position"`
	MoveStatus CustomMoveStatus `xml:"http://www.onvif.org/ver10/schema MoveStatus"`
	UtcTime    string           `xml:"http://www.onvif.org/ver10/schema UtcTime"`
}

type CustomPosition struct {
	PanTilt CustomVector2D `xml:"http://www.onvif.org/ver10/schema PanTilt"`
	Zoom    CustomVector1D `xml:"http://www.onvif.org/ver10/schema Zoom"`
}

type CustomVector2D struct {
	X     float64 `xml:"x,attr"`
	Y     float64 `xml:"y,attr"`
	Space string  `xml:"space,attr,omitempty"`
}

type CustomVector1D struct {
	X     float64 `xml:"x,attr"`
	Space string  `xml:"space,attr,omitempty"`
}

type CustomMoveStatus struct {
	PanTilt string `xml:"http://www.onvif.org/ver10/schema PanTilt"`
	Zoom    string `xml:"http://www.onvif.org/ver10/schema Zoom"`
}

// --- Custom Structs for GetProfiles Response ---
type ProfilesEnvelope struct {
	XMLName xml.Name `xml:"http://www.w3.org/2003/05/soap-envelope Envelope"`
	Body    struct {
		GetProfilesResponse struct {
			Profiles []struct {
				Token string `xml:"token,attr"`
			} `xml:"Profiles"`
		} `xml:"GetProfilesResponse"`
	} `xml:"Body"`
}

package onvifptzclient

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"

	"go.viam.com/rdk/components/generic"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/resource"
	"go.viam.com/utils/rpc"

	"github.com/use-go/onvif"
	"github.com/use-go/onvif/media"
	"github.com/use-go/onvif/ptz"
	"github.com/use-go/onvif/xsd"
	onvifxsd "github.com/use-go/onvif/xsd/onvif"
)

var (
	Client           = resource.NewModel("seanorg", "onvif-ptz-client", "client")
	errUnimplemented = errors.New("unimplemented")
)

func init() {
	resource.RegisterComponent(generic.API, Client,
		resource.Registration[resource.Resource, *Config]{
			Constructor: newOnvifPtzClientClient,
		},
	)
}

type Config struct {
	Address      string `json:"address"`
	Username     string `json:"username"`
	Password     string `json:"password"`
	ProfileToken string `json:"profile_token"`
}

func (cfg *Config) Validate(path string) ([]string, error) {
	if cfg.Address == "" {
		return nil, fmt.Errorf(`expected "address" attribute for ONVIF PTZ client %q`, path)
	}

	if cfg.Username == "" {
		return nil, fmt.Errorf(`expected "username" attribute for ONVIF PTZ client %q`, path)
	}
	if cfg.Password == "" {
		return nil, fmt.Errorf(`expected "password" attribute for ONVIF PTZ client %q`, path)
	}
	return nil, nil
}

type onvifPtzClientClient struct {
	resource.AlwaysRebuild

	name   resource.Name
	logger logging.Logger
	cfg    *Config
	dev    *onvif.Device // ONVIF device instance

	cancelCtx  context.Context
	cancelFunc func()
}

func newOnvifPtzClientClient(ctx context.Context, deps resource.Dependencies, rawConf resource.Config, logger logging.Logger) (resource.Resource, error) {
	conf, err := resource.NativeConfig[*Config](rawConf)
	if err != nil {
		return nil, err
	}

	return NewClient(ctx, deps, rawConf.ResourceName(), conf, logger)
}

func NewClient(ctx context.Context, deps resource.Dependencies, name resource.Name, conf *Config, logger logging.Logger) (resource.Resource, error) {
	cancelCtx, cancelFunc := context.WithCancel(context.Background())

	logger.Infof("Attempting to connect to ONVIF device at %s", conf.Address)
	dev, err := onvif.NewDevice(onvif.DeviceParams{
		Xaddr:    conf.Address,
		Username: conf.Username,
		Password: conf.Password,
	})
	if err != nil {
		cancelFunc()
		return nil, fmt.Errorf("failed to create ONVIF device for %s: %w", conf.Address, err)
	}
	logger.Info("Successfully connected to ONVIF device.")

	s := &onvifPtzClientClient{
		name:       name,
		logger:     logger,
		cfg:        conf,
		dev:        dev,
		cancelCtx:  cancelCtx,
		cancelFunc: cancelFunc,
	}
	if s.cfg.ProfileToken == "" {
		logger.Warn("No 'profile_token' configured. PTZ commands may fail. Run 'get-profiles' to discover available profiles.")
	}

	return s, nil
}

func (s *onvifPtzClientClient) Name() resource.Name {
	return s.name
}

func (s *onvifPtzClientClient) NewClientFromConn(ctx context.Context, conn rpc.ClientConn, remoteName string, name resource.Name, logger logging.Logger) (resource.Resource, error) {
	panic("not implemented")
}

// handleGetProfiles retrieves available media profiles from the camera and implements the get-profiles command logic.
func (s *onvifPtzClientClient) handleGetProfiles() (map[string]interface{}, error) {
	s.logger.Debug("Fetching media profiles...")
	req := media.GetProfiles{}
	res, err := s.dev.CallMethod(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call GetProfiles: %w", err)
	}
	defer res.Body.Close()

	bodyBytes, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read GetProfiles response body: %w", err)
	}

	var envelope ProfilesEnvelope
	err = xml.Unmarshal(bodyBytes, &envelope)
	if err != nil {
		s.logger.Warnf("Failed to unmarshal GetProfiles response. Raw XML:\n%s", string(bodyBytes))
		return nil, fmt.Errorf("failed to unmarshal GetProfiles response: %w", err)
	}

	var tokens []string
	for _, p := range envelope.Body.GetProfilesResponse.Profiles {
		tokens = append(tokens, p.Token)
	}
	s.logger.Debugf("Found profiles: %v", tokens)
	return map[string]interface{}{"profiles": tokens}, nil
}

// handleGetStatus implements the get-status command logic
func (s *onvifPtzClientClient) handleGetStatus() (map[string]interface{}, error) {
	if s.cfg.ProfileToken == "" {
		return nil, errors.New("profile_token is not configured for this component")
	}
	profileToken := onvifxsd.ReferenceToken(s.cfg.ProfileToken)

	req := ptz.GetStatus{ProfileToken: profileToken}
	s.logger.Debugf("Sending GetStatus request for profile: %s", profileToken)

	res, err := s.dev.CallMethod(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call GetStatus: %w", err)
	}
	defer res.Body.Close()

	bodyBytes, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read GetStatus response body: %w", err)
	}

	var statusEnvelope CustomGetStatusEnvelope
	err = xml.Unmarshal(bodyBytes, &statusEnvelope)
	if err != nil {
		s.logger.Warnf("Failed to unmarshal GetStatus response using custom structs. Raw XML:\n%s", string(bodyBytes))
		return nil, fmt.Errorf("failed to unmarshal GetStatus response: %w", err)
	}

	ptzStatus := statusEnvelope.Body.GetResponse.PTZStatus

	// Return status as a map matching the struct fields for easy JSON serialization
	return map[string]interface{}{
		"position": map[string]interface{}{
			"pan_tilt": map[string]interface{}{
				"x":     ptzStatus.Position.PanTilt.X,
				"y":     ptzStatus.Position.PanTilt.Y,
				"space": ptzStatus.Position.PanTilt.Space,
			},
			"zoom": map[string]interface{}{
				"x":     ptzStatus.Position.Zoom.X,
				"space": ptzStatus.Position.Zoom.Space,
			},
		},
		"move_status": map[string]interface{}{
			"pan_tilt": ptzStatus.MoveStatus.PanTilt,
			"zoom":     ptzStatus.MoveStatus.Zoom,
		},
		"utc_time": ptzStatus.UtcTime,
	}, nil
}

// handleStop implements the stop command logic
func (s *onvifPtzClientClient) handleStop(cmd map[string]interface{}) (map[string]interface{}, error) {
	if s.cfg.ProfileToken == "" {
		return nil, errors.New("profile_token is not configured for this component")
	}
	profileToken := onvifxsd.ReferenceToken(s.cfg.ProfileToken)

	stopPanTilt := getOptionalBool(cmd, "pan_tilt", true) // Default to true
	stopZoom := getOptionalBool(cmd, "zoom", true)        // Default to true

	req := ptz.Stop{
		ProfileToken: profileToken,
		PanTilt:      xsd.Boolean(stopPanTilt),
		Zoom:         xsd.Boolean(stopZoom),
	}

	s.logger.Debugf("Sending Stop command (PanTilt: %v, Zoom: %v) for profile %s...", stopPanTilt, stopZoom, profileToken)
	_, err := s.dev.CallMethod(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call Stop: %w", err)
	}
	s.logger.Infof("Stop command sent successfully for profile %s.", profileToken)
	return map[string]interface{}{"success": true}, nil
}

// handleContinuousMove implements the continuous-move command logic
func (s *onvifPtzClientClient) handleContinuousMove(cmd map[string]interface{}) (map[string]interface{}, error) {
	if s.cfg.ProfileToken == "" {
		return nil, errors.New("profile_token is not configured for this component")
	}
	profileToken := onvifxsd.ReferenceToken(s.cfg.ProfileToken)

	panSpeed := getOptionalFloat64(cmd, "pan_speed", 0.0)
	tiltSpeed := getOptionalFloat64(cmd, "tilt_speed", 0.0)
	zoomSpeed := getOptionalFloat64(cmd, "zoom_speed", 0.0)

	if panSpeed < -1.0 || panSpeed > 1.0 || tiltSpeed < -1.0 || tiltSpeed > 1.0 || zoomSpeed < -1.0 || zoomSpeed > 1.0 {
		return nil, fmt.Errorf("speed values (pan_speed, tilt_speed, zoom_speed) must be between -1.0 and 1.0")
	}

	req := ptz.ContinuousMove{
		ProfileToken: profileToken,
		Velocity: onvifxsd.PTZSpeed{
			PanTilt: onvifxsd.Vector2D{
				X:     panSpeed,
				Y:     tiltSpeed,
				Space: ContinuousPanTiltVelocityGenericSpace, // Specify space
			},
			Zoom: onvifxsd.Vector1D{
				X:     zoomSpeed,
				Space: ContinuousZoomVelocityGenericSpace, // Specify space
			},
		},
		// Timeout: // Timeout handling might be complex here
	}

	s.logger.Debugf("Sending ContinuousMove (PanSpeed: %.2f, TiltSpeed: %.2f, ZoomSpeed: %.2f) for profile %s...", panSpeed, tiltSpeed, zoomSpeed, profileToken)
	_, err := s.dev.CallMethod(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call ContinuousMove: %w", err)
	}

	s.logger.Infof("ContinuousMove command sent successfully for profile %s. Send 'stop' command to halt.", profileToken)
	return map[string]interface{}{"success": true}, nil
}

// handleRelativeMove implements the relative-move command logic
func (s *onvifPtzClientClient) handleRelativeMove(cmd map[string]interface{}) (map[string]interface{}, error) {
	if s.cfg.ProfileToken == "" {
		return nil, errors.New("profile_token is not configured for this component")
	}
	profileToken := onvifxsd.ReferenceToken(s.cfg.ProfileToken)

	panRelative := getOptionalFloat64(cmd, "pan", 0.0)
	tiltRelative := getOptionalFloat64(cmd, "tilt", 0.0)
	zoomRelative := getOptionalFloat64(cmd, "zoom", 0.0)
	useDegrees := getOptionalBool(cmd, "degrees", false)

	speedX := getOptionalFloat64(cmd, "speed_pan", 0.5)
	speedY := getOptionalFloat64(cmd, "speed_tilt", 0.5)
	speedZ := getOptionalFloat64(cmd, "speed_zoom", 0.5)
	useSpeed := getOptionalBool(cmd, "use_speed", false) // Check if speed args were provided

	// Input validation based on degrees flag
	if useDegrees {
		if panRelative < -180.0 || panRelative > 180.0 {
			return nil, errors.New("relative pan must be between -180.0 and 180.0 when using degrees")
		}
		if tiltRelative < -90.0 || tiltRelative > 90.0 {
			return nil, errors.New("relative tilt must be between -90.0 and 90.0 when using degrees")
		}
	} else {
		if panRelative < -1.0 || panRelative > 1.0 {
			return nil, errors.New("relative pan must be between -1.0 and 1.0 (use degrees=true for degrees)")
		}
		if tiltRelative < -1.0 || tiltRelative > 1.0 {
			return nil, errors.New("relative tilt must be between -1.0 and 1.0 (use degrees=true for degrees)")
		}
	}
	if zoomRelative < -1.0 || zoomRelative > 1.0 {
		return nil, errors.New("relative zoom must be between -1.0 and 1.0")
	}

	panTiltVector := onvifxsd.Vector2D{
		X: panRelative,
		Y: tiltRelative,
	}
	if useDegrees {
		panTiltVector.Space = RelativePanTiltTranslationSphericalDegrees
		s.logger.Debug("Using Spherical Degrees space for relative Pan/Tilt.")
	} else {
		panTiltVector.Space = RelativePanTiltTranslationGenericSpace
		s.logger.Debug("Using Generic Normalized space for relative Pan/Tilt.")
	}

	req := ptz.RelativeMove{
		ProfileToken: profileToken,
		Translation: onvifxsd.PTZVector{
			PanTilt: panTiltVector,
			Zoom: onvifxsd.Vector1D{
				X:     zoomRelative,
				Space: RelativeZoomTranslationGenericSpace, // Zoom always generic space for relative
			},
		},
	}

	if useSpeed {
		if speedX < -1.0 || speedX > 1.0 || speedY < -1.0 || speedY > 1.0 || speedZ < -1.0 || speedZ > 1.0 {
			return nil, errors.New("speed values must be between -1.0 and 1.0")
		}
		req.Speed = onvifxsd.PTZSpeed{
			PanTilt: onvifxsd.Vector2D{X: speedX, Y: speedY},
			Zoom:    onvifxsd.Vector1D{X: speedZ},
		}
		s.logger.Debugf("Sending RelativeMove (Pan: %.3f, Tilt: %.3f, Zoom: %.3f) with Speed (X: %.2f, Y: %.2f, Z: %.2f) for profile %s...",
			panRelative, tiltRelative, zoomRelative, speedX, speedY, speedZ, profileToken)
	} else {
		s.logger.Debugf("Sending RelativeMove (Pan: %.3f, Tilt: %.3f, Zoom: %.3f) with default speed for profile %s...",
			panRelative, tiltRelative, zoomRelative, profileToken)
	}

	_, err := s.dev.CallMethod(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call RelativeMove: %w", err)
	}
	s.logger.Infof("RelativeMove command sent successfully for profile %s.", profileToken)
	return map[string]interface{}{"success": true}, nil
}

// handleAbsoluteMove implements the absolute-move command logic
func (s *onvifPtzClientClient) handleAbsoluteMove(cmd map[string]interface{}) (map[string]interface{}, error) {
	if s.cfg.ProfileToken == "" {
		return nil, errors.New("profile_token is not configured for this component")
	}
	profileToken := onvifxsd.ReferenceToken(s.cfg.ProfileToken)

	// For absolute move, position parameters are mandatory
	var panAbsolute, tiltAbsolute, zoomAbsolute float64
	var err error

	panAbsolute, err = getFloat64(cmd, "pan")
	if err != nil {
		return nil, err
	}
	tiltAbsolute, err = getFloat64(cmd, "tilt")
	if err != nil {
		return nil, err
	}
	zoomAbsolute, err = getFloat64(cmd, "zoom")
	if err != nil {
		return nil, err
	}
	useDegrees := getOptionalBool(cmd, "degrees", false)

	speedX := getOptionalFloat64(cmd, "speed_pan", 0.5)
	speedY := getOptionalFloat64(cmd, "speed_tilt", 0.5)
	speedZ := getOptionalFloat64(cmd, "speed_zoom", 0.5)
	useSpeed := getOptionalBool(cmd, "use_speed", false) // Check if speed args were provided

	// Input validation based on degrees flag
	if useDegrees {
		if panAbsolute < -180.0 || panAbsolute > 180.0 {
			return nil, errors.New("absolute pan must be between -180.0 and 180.0 when using degrees")
		}
		if tiltAbsolute < -90.0 || tiltAbsolute > 90.0 {
			return nil, errors.New("absolute tilt must be between -90.0 and 90.0 when using degrees")
		}
	} else {
		if panAbsolute < -1.0 || panAbsolute > 1.0 {
			return nil, errors.New("absolute pan must be between -1.0 and 1.0 (use degrees=true for degrees)")
		}
		if tiltAbsolute < -1.0 || tiltAbsolute > 1.0 {
			return nil, errors.New("absolute tilt must be between -1.0 and 1.0 (use degrees=true for degrees)")
		}
	}
	if zoomAbsolute < 0.0 || zoomAbsolute > 1.0 {
		return nil, errors.New("absolute zoom must be between 0.0 and 1.0")
	}

	panTiltVector := onvifxsd.Vector2D{
		X: panAbsolute,
		Y: tiltAbsolute,
	}
	if useDegrees {
		panTiltVector.Space = AbsolutePanTiltPositionSphericalDegrees
		s.logger.Debug("Using Spherical Degrees space for absolute Pan/Tilt.")
	} else {
		panTiltVector.Space = AbsolutePanTiltPositionGenericSpace
		s.logger.Debug("Using Generic Normalized space for absolute Pan/Tilt.")
	}

	req := ptz.AbsoluteMove{
		ProfileToken: profileToken,
		Position: onvifxsd.PTZVector{
			PanTilt: panTiltVector,
			Zoom: onvifxsd.Vector1D{
				X:     zoomAbsolute,
				Space: AbsoluteZoomPositionGenericSpace, // Zoom always generic space for absolute
			},
		},
	}

	if useSpeed {
		if speedX < -1.0 || speedX > 1.0 || speedY < -1.0 || speedY > 1.0 || speedZ < -1.0 || speedZ > 1.0 {
			return nil, errors.New("speed values must be between -1.0 and 1.0")
		}
		req.Speed = onvifxsd.PTZSpeed{
			PanTilt: onvifxsd.Vector2D{X: speedX, Y: speedY},
			Zoom:    onvifxsd.Vector1D{X: speedZ},
		}
		s.logger.Debugf("Sending AbsoluteMove (Pan: %.3f, Tilt: %.3f, Zoom: %.3f) with Speed (X: %.2f, Y: %.2f, Z: %.2f) for profile %s...",
			panAbsolute, tiltAbsolute, zoomAbsolute, speedX, speedY, speedZ, profileToken)
	} else {
		s.logger.Debugf("Sending AbsoluteMove (Pan: %.3f, Tilt: %.3f, Zoom: %.3f) with default speed for profile %s...",
			panAbsolute, tiltAbsolute, zoomAbsolute, profileToken)
	}

	_, err = s.dev.CallMethod(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call AbsoluteMove: %w", err)
	}
	s.logger.Infof("AbsoluteMove command sent successfully for profile %s.", profileToken)
	return map[string]interface{}{"success": true}, nil
}

// DoCommand maps incoming commands to the appropriate ONVIF PTZ action
func (s *onvifPtzClientClient) DoCommand(ctx context.Context, cmd map[string]interface{}) (map[string]interface{}, error) {
	command, err := getString(cmd, "command")
	if err != nil {
		return nil, errors.New("invalid command request: 'command' key missing or not a string")
	}

	s.logger.Debugf("Received command: %s with args: %v", command, cmd)

	switch strings.ToLower(command) {
	case "get-profiles":
		return s.handleGetProfiles()
	case "get-status":
		return s.handleGetStatus()
	case "stop":
		return s.handleStop(cmd)
	case "continuous-move":
		return s.handleContinuousMove(cmd)
	case "relative-move":
		return s.handleRelativeMove(cmd)
	case "absolute-move":
		return s.handleAbsoluteMove(cmd)
	default:
		return nil, fmt.Errorf("unrecognized DoCommand command: %s", command)
	}
}

func (s *onvifPtzClientClient) Close(context.Context) error {
	s.cancelFunc()
	return nil
}

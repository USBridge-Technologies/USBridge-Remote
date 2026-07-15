#include <TargetConditionals.h>
#if TARGET_OS_IPHONE

#import <AVFoundation/AVFoundation.h>
#import <UIKit/UIKit.h>
#import <stdatomic.h>
#import <string.h>

// Shared state polled from Go goroutine (0=idle, 1=result, 2=cancelled)
static _Atomic int _qr_status = 0;
static char _qr_buf[4096];

void ios_qr_get_status(int *status_out, char *buf, int buflen) {
    *status_out = atomic_load(&_qr_status);
    if (*status_out == 1) {
        strncpy(buf, _qr_buf, buflen - 1);
        buf[buflen - 1] = '\0';
    }
}

void ios_qr_clear(void) {
    atomic_store(&_qr_status, 0);
    memset(_qr_buf, 0, sizeof(_qr_buf));
}

// ---- View Controller ----

@interface _USBridgeQRVC : UIViewController <AVCaptureMetadataOutputObjectsDelegate>
@property (nonatomic, strong) AVCaptureSession *session;
@property (nonatomic, strong) AVCaptureVideoPreviewLayer *preview;
@property (nonatomic, assign) BOOL found;
@end

@implementation _USBridgeQRVC

- (void)viewDidLoad {
    [super viewDidLoad];
    self.view.backgroundColor = [UIColor blackColor];
    [self setupCamera];
    [self setupOverlay];
}

- (void)viewDidLayoutSubviews {
    [super viewDidLayoutSubviews];
    self.preview.frame = self.view.bounds;
}

- (void)setupCamera {
    AVCaptureDevice *device = [AVCaptureDevice defaultDeviceWithMediaType:AVMediaTypeVideo];
    if (!device) { [self doCancel]; return; }

    NSError *err = nil;
    AVCaptureDeviceInput *input = [AVCaptureDeviceInput deviceInputWithDevice:device error:&err];
    if (!input) { [self doCancel]; return; }

    self.session = [[AVCaptureSession alloc] init];
    if (![self.session canAddInput:input]) { [self doCancel]; return; }
    [self.session addInput:input];

    AVCaptureMetadataOutput *output = [[AVCaptureMetadataOutput alloc] init];
    if (![self.session canAddOutput:output]) { [self doCancel]; return; }
    [self.session addOutput:output];
    [output setMetadataObjectsDelegate:self queue:dispatch_get_main_queue()];
    output.metadataObjectTypes = @[AVMetadataObjectTypeQRCode];

    self.preview = [AVCaptureVideoPreviewLayer layerWithSession:self.session];
    self.preview.frame = self.view.bounds;
    self.preview.videoGravity = AVLayerVideoGravityResizeAspectFill;
    [self.view.layer insertSublayer:self.preview atIndex:0];

    [self.session startRunning];
}

- (void)setupOverlay {
    UILabel *hint = [[UILabel alloc] init];
    hint.text = @"Point camera at QR code";
    hint.textColor = [UIColor whiteColor];
    hint.textAlignment = NSTextAlignmentCenter;
    hint.backgroundColor = [UIColor colorWithWhite:0 alpha:0.5];
    hint.layer.cornerRadius = 8;
    hint.layer.masksToBounds = YES;
    hint.translatesAutoresizingMaskIntoConstraints = NO;
    [self.view addSubview:hint];

    UIButton *cancelBtn = [UIButton buttonWithType:UIButtonTypeSystem];
    [cancelBtn setTitle:@"Cancel" forState:UIControlStateNormal];
    cancelBtn.titleLabel.font = [UIFont systemFontOfSize:18 weight:UIFontWeightSemibold];
    [cancelBtn setTitleColor:[UIColor whiteColor] forState:UIControlStateNormal];
    cancelBtn.backgroundColor = [UIColor colorWithWhite:0 alpha:0.5];
    cancelBtn.layer.cornerRadius = 12;
    cancelBtn.layer.masksToBounds = YES;
    cancelBtn.contentEdgeInsets = UIEdgeInsetsMake(10, 24, 10, 24);
    cancelBtn.translatesAutoresizingMaskIntoConstraints = NO;
    [self.view addSubview:cancelBtn];
    [cancelBtn addTarget:self action:@selector(doCancel) forControlEvents:UIControlEventTouchUpInside];

#pragma clang diagnostic push
#pragma clang diagnostic ignored "-Wunguarded-availability-new"
    [NSLayoutConstraint activateConstraints:@[
        [hint.topAnchor constraintEqualToAnchor:self.view.safeAreaLayoutGuide.topAnchor constant:24],
        [hint.centerXAnchor constraintEqualToAnchor:self.view.centerXAnchor],
        [hint.leadingAnchor constraintGreaterThanOrEqualToAnchor:self.view.leadingAnchor constant:16],
        [hint.trailingAnchor constraintLessThanOrEqualToAnchor:self.view.trailingAnchor constant:-16],
        [cancelBtn.bottomAnchor constraintEqualToAnchor:self.view.safeAreaLayoutGuide.bottomAnchor constant:-32],
        [cancelBtn.centerXAnchor constraintEqualToAnchor:self.view.centerXAnchor],
    ]];
#pragma clang diagnostic pop
}

- (void)captureOutput:(AVCaptureOutput *)output
didOutputMetadataObjects:(NSArray<__kindof AVMetadataObject *> *)objects
       fromConnection:(AVCaptureConnection *)connection {
    if (self.found) return;
    for (AVMetadataObject *obj in objects) {
        if (![obj isKindOfClass:[AVMetadataMachineReadableCodeObject class]]) continue;
        AVMetadataMachineReadableCodeObject *code = (AVMetadataMachineReadableCodeObject *)obj;
        NSString *val = code.stringValue;
        if (!val || val.length == 0) continue;

        self.found = YES;
        [self.session stopRunning];

        const char *utf8 = [val UTF8String];
        strncpy(_qr_buf, utf8, sizeof(_qr_buf) - 1);
        _qr_buf[sizeof(_qr_buf) - 1] = '\0';
        atomic_store(&_qr_status, 1);

        dispatch_async(dispatch_get_main_queue(), ^{
            [self dismissViewControllerAnimated:YES completion:nil];
        });
        return;
    }
}

- (void)doCancel {
    if (self.found) return;
    self.found = YES;
    if (self.session.isRunning) [self.session stopRunning];
    atomic_store(&_qr_status, 2);
    dispatch_async(dispatch_get_main_queue(), ^{
        [self dismissViewControllerAnimated:YES completion:nil];
    });
}

@end

// ---- Public C API ----

static void _usbridge_present_qr_vc(void) {
    UIViewController *root = nil;
    if (@available(iOS 13.0, *)) {
        for (UIScene *scene in [UIApplication sharedApplication].connectedScenes) {
            if (![scene isKindOfClass:[UIWindowScene class]]) continue;
            UIWindowScene *ws = (UIWindowScene *)scene;
            for (UIWindow *win in ws.windows) {
                if (win.isKeyWindow) { root = win.rootViewController; break; }
            }
            if (root) break;
        }
    }
    if (!root) {
#pragma clang diagnostic push
#pragma clang diagnostic ignored "-Wdeprecated-declarations"
        root = [UIApplication sharedApplication].keyWindow.rootViewController;
#pragma clang diagnostic pop
    }
    if (!root) { atomic_store(&_qr_status, 2); return; }

    while (root.presentedViewController) root = root.presentedViewController;

    _USBridgeQRVC *vc = [[_USBridgeQRVC alloc] init];
    vc.modalPresentationStyle = UIModalPresentationFullScreen;
    [root presentViewController:vc animated:YES completion:nil];
}

void ios_launch_qr_scanner(void) {
    ios_qr_clear();

    dispatch_async(dispatch_get_main_queue(), ^{
        AVAuthorizationStatus status = [AVCaptureDevice authorizationStatusForMediaType:AVMediaTypeVideo];
        if (status == AVAuthorizationStatusAuthorized) {
            _usbridge_present_qr_vc();
            return;
        }
        if (status == AVAuthorizationStatusNotDetermined) {
            [AVCaptureDevice requestAccessForMediaType:AVMediaTypeVideo completionHandler:^(BOOL granted) {
                dispatch_async(dispatch_get_main_queue(), ^{
                    if (granted) {
                        _usbridge_present_qr_vc();
                    } else {
                        atomic_store(&_qr_status, 2);
                    }
                });
            }];
            return;
        }
        // Denied or restricted
        atomic_store(&_qr_status, 2);
    });
}

#endif // TARGET_OS_IPHONE

#import <Foundation/Foundation.h>

void nebula_disable_automatic_termination(void) {
    @autoreleasepool {
        [[NSProcessInfo processInfo]
            disableAutomaticTermination:@"Nebula Tray must remain available"];
    }
}

// Shared by USBridgeAgent.iss and the all-in-one USBridge.iss (#include in
// their [Code]): whether this machine has a real GPU, which decides the
// default of the virtual display driver checkbox -- on with a real GPU (a
// VM's passed-through one included), off on a VM whose only adapters are
// virtual, where the extra monitor extends the desktop off the VM window
// (mouse offset, windows opening out of sight).

var
  RealGpuChecked, RealGpu: Boolean;

// PCI vendor ids of VM display adapters: Microsoft Basic Render/Hyper-V,
// VMware, virtio-gpu (Red Hat), VirtualBox, Parallels, QEMU/Bochs VGA, Cirrus.
function IsVirtualDisplayVendor(const Vendor: String): Boolean;
begin
  Result := (Vendor = '1414') or (Vendor = '15AD') or (Vendor = '1AF4') or (Vendor = '80EE') or
            (Vendor = '1AB8') or (Vendor = '1234') or (Vendor = '1013');
end;

// True when some display adapter is a real PCI GPU, a VM's passed-through one
// included; false when every adapter is a VM's virtual one (or not on PCI at
// all, like Hyper-V's synthetic video). True whenever WMI can't tell.
function HasRealGpu(): Boolean;
var
  Locator, Service, Items, Item: Variant;
  I: Integer;
  Id: String;
begin
  if not RealGpuChecked then begin
    RealGpuChecked := True;
    RealGpu := True;
    try
      Locator := CreateOleObject('WbemScripting.SWbemLocator');
      Service := Locator.ConnectServer('.', 'root\CIMV2');
      Items := Service.ExecQuery('SELECT PNPDeviceID FROM Win32_VideoController');
      RealGpu := False;
      for I := 0 to Items.Count - 1 do begin
        Item := Items.ItemIndex(I);
        Id := Uppercase(Item.PNPDeviceID);
        Log('Display adapter: ' + Id);
        if (Pos('PCI\VEN_', Id) = 1) and not IsVirtualDisplayVendor(Copy(Id, 9, 4)) then
          RealGpu := True;
      end;
    except
      Log('Display adapters: WMI query failed, offering the virtual display driver');
      RealGpu := True;
    end;
  end;
  Result := RealGpu;
end;

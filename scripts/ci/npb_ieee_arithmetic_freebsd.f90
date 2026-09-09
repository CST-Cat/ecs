! Build-only compatibility module for FreeBSD/aarch64 gfortran.
! FreeBSD's GCC packages do not ship the ieee_arithmetic intrinsic module on
! aarch64. NPB 3.4.4 only needs ieee_is_nan from that module. The builder
! installs this module through gfortran's native -fintrinsic-modules-path
! mechanism, so the pinned upstream NPB sources remain byte-for-byte intact.
module ieee_arithmetic
  implicit none
  private
  public :: ieee_is_nan

  interface ieee_is_nan
    module procedure ieee_is_nan_real32
    module procedure ieee_is_nan_real64
  end interface ieee_is_nan

contains

  pure elemental logical function ieee_is_nan_real32(value)
    real(kind=kind(0.0)), intent(in) :: value
    ieee_is_nan_real32 = value /= value
  end function ieee_is_nan_real32

  pure elemental logical function ieee_is_nan_real64(value)
    real(kind=kind(0.0d0)), intent(in) :: value
    ieee_is_nan_real64 = value /= value
  end function ieee_is_nan_real64

end module ieee_arithmetic

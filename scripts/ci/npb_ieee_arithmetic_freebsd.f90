! Build-only compatibility module for FreeBSD/aarch64 gfortran.
! FreeBSD's GCC packages do not ship the ieee_arithmetic intrinsic module on
! aarch64. NPB 3.4.4 only needs ieee_is_nan from that module. The builder
! installs this module through gfortran's native -fintrinsic-modules-path
! mechanism, so the pinned upstream NPB sources remain byte-for-byte intact.
!
! gfortran also emits calls to its IEEE procedure entry/exit runtime hooks for
! any procedure that USEs an IEEE intrinsic module. Those libgfortran symbols
! are absent on FreeBSD/aarch64 for the same incomplete IEEE support. Provide
! the two ABI hooks here using FreeBSD's native fenv(3) API. This mirrors
! libgfortran's upstream semantics: save/clear on entry, restore/re-raise on
! exit. gfortran reserves 32 bytes for the state buffer; FreeBSD/aarch64's
! fenv_t is 8 bytes.
module ieee_arithmetic
  use, intrinsic :: iso_c_binding, only : c_int, c_ptr
  implicit none
  private
  public :: ieee_is_nan

  integer(c_int), parameter :: fe_all_except = 31_c_int

  interface ieee_is_nan
    module procedure ieee_is_nan_real32
    module procedure ieee_is_nan_real64
  end interface ieee_is_nan

  interface
    function c_fegetenv(env) bind(C, name="fegetenv") result(rc)
      import :: c_int, c_ptr
      type(c_ptr), value :: env
      integer(c_int) :: rc
    end function c_fegetenv

    function c_feclearexcept(excepts) bind(C, name="feclearexcept") result(rc)
      import :: c_int
      integer(c_int), value :: excepts
      integer(c_int) :: rc
    end function c_feclearexcept

    function c_fetestexcept(excepts) bind(C, name="fetestexcept") result(flags)
      import :: c_int
      integer(c_int), value :: excepts
      integer(c_int) :: flags
    end function c_fetestexcept

    function c_fesetenv(env) bind(C, name="fesetenv") result(rc)
      import :: c_int, c_ptr
      type(c_ptr), value :: env
      integer(c_int) :: rc
    end function c_fesetenv

    function c_feraiseexcept(excepts) bind(C, name="feraiseexcept") result(rc)
      import :: c_int
      integer(c_int), value :: excepts
      integer(c_int) :: rc
    end function c_feraiseexcept
  end interface

contains

  pure elemental logical function ieee_is_nan_real32(value)
    real(kind=kind(0.0)), intent(in) :: value
    ieee_is_nan_real32 = value /= value
  end function ieee_is_nan_real32

  pure elemental logical function ieee_is_nan_real64(value)
    real(kind=kind(0.0d0)), intent(in) :: value
    ieee_is_nan_real64 = value /= value
  end function ieee_is_nan_real64

  subroutine gfortran_ieee_procedure_entry(state) &
      bind(C, name="_gfortran_ieee_procedure_entry")
    type(c_ptr), value :: state
    integer(c_int) :: ignored

    ignored = c_fegetenv(state)
    ignored = c_feclearexcept(fe_all_except)
  end subroutine gfortran_ieee_procedure_entry

  subroutine gfortran_ieee_procedure_exit(state) &
      bind(C, name="_gfortran_ieee_procedure_exit")
    type(c_ptr), value :: state
    integer(c_int) :: flags, ignored

    flags = c_fetestexcept(fe_all_except)
    ignored = c_fesetenv(state)
    if (flags > 0_c_int) ignored = c_feraiseexcept(flags)
  end subroutine gfortran_ieee_procedure_exit

end module ieee_arithmetic

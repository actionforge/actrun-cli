/*
 * Copyright 2005 Perforce Software.  All rights reserved.
 *
 * This file is part of Perforce - the FAST SCM System.
 */

/*
 * GlobalCharSet -- a static charSet index across the process.
 */

class GlobalCharSet {

    public:
	static void	Set( int cs = 0 );
	static int	Get();
	// Switch to using a per-thread version of the charset.  Used
	// in contexts where code may temporarily want to change the
	// charset in an incompatible way to the expectations of the
	// larger process.
	static void	UseAlt( const bool val );
  
    private:
	static int	globCharSet;
  	MT_STATIC bool	globCharSetUseAlt;
    	MT_STATIC int	globCharSetAlt;
} ;
